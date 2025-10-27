package bot

import (
	"fmt"
	"log/slog"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/hectorgimenez/koolo/cmd/koolo/log"
	"github.com/hectorgimenez/koolo/internal/character"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/event"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/health"
	"github.com/hectorgimenez/koolo/internal/mule"
	"github.com/hectorgimenez/koolo/internal/pather"
	"github.com/hectorgimenez/koolo/internal/utils"
	"github.com/hectorgimenez/koolo/internal/utils/winproc"
	"github.com/lxn/win"
)

type SupervisorManager struct {
	logger         *slog.Logger
	supervisors    map[string]Supervisor
	crashDetectors map[string]*game.CrashDetector
	eventListener  *event.Listener
}

func NewSupervisorManager(logger *slog.Logger, eventListener *event.Listener) *SupervisorManager {

	return &SupervisorManager{
		logger:         logger,
		supervisors:    make(map[string]Supervisor),
		crashDetectors: make(map[string]*game.CrashDetector),
		eventListener:  eventListener,
	}
}

func (mng *SupervisorManager) AvailableSupervisors() []string {
	availableSupervisors := make([]string, 0)
	for name := range config.GetCharacters() {
		if name != "template" {
			availableSupervisors = append(availableSupervisors, name)
		}
	}

	return availableSupervisors
}

func (mng *SupervisorManager) Start(supervisorName string, attachToExisting bool, pidHwnd ...uint32) error {
	// Avoid multiple instances of the supervisor - shitstorm prevention
	if _, exists := mng.supervisors[supervisorName]; exists {
		return fmt.Errorf("supervisor %s is already running", supervisorName)
	}

	// Reload config to get the latest local changes before starting the supervisor
	err := config.Load()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	supervisorLogger, err := log.NewLogger(config.Koolo.Debug.Log, config.Koolo.LogSaveDirectory, supervisorName)
	if err != nil {
		return err
	}

	var optionalPID uint32
	var optionalHWND win.HWND

	if attachToExisting {
		if len(pidHwnd) == 2 {
			mng.logger.Info("Attaching to existing game", "pid", pidHwnd[0], "hwnd", pidHwnd[1])
			optionalPID = pidHwnd[0]
			optionalHWND = win.HWND(pidHwnd[1])
		} else {
			return fmt.Errorf("pid and hwnd are required when attaching to an existing game")
		}
	}

	supervisor, crashDetector, err := mng.buildSupervisor(supervisorName, supervisorLogger, attachToExisting, optionalPID, optionalHWND)
	if err != nil {
		return err
	}

	if oldCrashDetector, exists := mng.crashDetectors[supervisorName]; exists {
		oldCrashDetector.Stop() // Stop the old crash detector if it exists
	}

	mng.supervisors[supervisorName] = supervisor
	mng.crashDetectors[supervisorName] = crashDetector

	if config.Koolo.GameWindowArrangement {
		go func() {
			// When the game starts, its doing some weird stuff like repositioning and resizing window automatically
			// we need to wait until this is done in order to reposition, or it will be overridden
			time.Sleep(time.Second * 5)
			mng.rearrangeWindows()
		}()
	}

	// Start the Crash Detector in a thread to avoid blocking and speed up start
	go crashDetector.Start()

	err = supervisor.Start()
	if err != nil {
		mng.logger.Error(fmt.Sprintf("error running supervisor %s: %s", supervisorName, err.Error()))
	}

	return nil
}

func (mng *SupervisorManager) ReloadConfig() error {

	// Load fresh configs
	if err := config.Load(); err != nil {
		return err
	}

	// Apply new configs to running supervisors
	for name, sup := range mng.supervisors {
		newCfg, exists := config.GetCharacter(name)
		if !exists {
			continue
		}

		ctx := sup.GetContext()
		if ctx == nil {
			continue
		}

		// Preserve runtime data
		//oldRuntimeData := ctx.CharacterCfg.Runtime

		// Update the config
		*ctx.CharacterCfg = *newCfg
		//ctx.CharacterCfg.Runtime = oldRuntimeData
	}

	return nil
}

func (mng *SupervisorManager) StopAll() {
	for _, s := range mng.supervisors {
		s.Stop()
	}
}

func (mng *SupervisorManager) Stop(supervisor string) {
	s, found := mng.supervisors[supervisor]
	if found {
		// Log the stop sequence
		mng.logger.Info("Stopping supervisor instance", slog.String("supervisor", supervisor))

		// Stop the Supervisor's internal loops and kill the client if configured
		s.Stop()

		// Delete from the list of active Supervisors
		delete(mng.supervisors, supervisor)

		// Stop the crash detector associated with it
		if cd, ok := mng.crashDetectors[supervisor]; ok {
			cd.Stop()
			delete(mng.crashDetectors, supervisor)
		}

		// The logic to start the next character has been removed from here.
		// The restartFunc is now the single source of truth for this,
		// preventing the mule from restarting itself.
	}
}

func (mng *SupervisorManager) TogglePause(supervisor string) {
	s, found := mng.supervisors[supervisor]
	if found {
		s.TogglePause()
	}
}

func (mng *SupervisorManager) Status(characterName string) Stats {
	for name, supervisor := range mng.supervisors {
		if name == characterName {
			return supervisor.Stats()
		}
	}

	return Stats{}
}

func (mng *SupervisorManager) GetData(characterName string) *game.Data {
	for name, supervisor := range mng.supervisors {
		if name == characterName {
			return supervisor.GetData()
		}
	}

	return nil
}

func (mng *SupervisorManager) GetContext(characterName string) *context.Context {
	for name, supervisor := range mng.supervisors {
		if name == characterName {
			return supervisor.GetContext()
		}
	}

	return nil
}

func (mng *SupervisorManager) buildSupervisor(supervisorName string, logger *slog.Logger, attach bool, optionalPID uint32, optionalHWND win.HWND) (Supervisor, *game.CrashDetector, error) {
	cfg, found := config.GetCharacter(supervisorName)
	if !found {
		return nil, nil, fmt.Errorf("character %s not found", supervisorName)
	}

	var pid uint32
	var hwnd win.HWND

	if attach {
		if optionalPID != 0 && optionalHWND != 0 {
			pid = optionalPID
			hwnd = optionalHWND
		} else {
			return nil, nil, fmt.Errorf("pid and hwnd are required when attaching to an existing game")
		}
	} else {
		var err error
		pid, hwnd, err = game.StartGame(cfg.Username, cfg.Password, cfg.AuthMethod, cfg.AuthToken, cfg.Realm, cfg.CommandLineArgs, config.Koolo.UseCustomSettings)
		if err != nil {
			return nil, nil, fmt.Errorf("error starting game: %w", err)
		}
	}

	gr, err := game.NewGameReader(cfg, supervisorName, pid, hwnd, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating game reader: %w", err)
	}

	gi, err := game.InjectorInit(logger, gr.GetPID())
	if err != nil {
		return nil, nil, fmt.Errorf("error creating game injector: %w", err)
	}

	ctx := context.NewContext(supervisorName)

	hidM := game.NewHID(gr, gi)
	pf := pather.NewPathFinder(gr, ctx.Data, hidM, cfg)

	bm := health.NewBeltManager(ctx.Data, hidM, logger, supervisorName)
	hm := health.NewHealthManager(bm, ctx.Data)

	ctx.CharacterCfg = cfg
	ctx.EventListener = mng.eventListener
	ctx.HID = hidM
	ctx.PacketSender = game.NewPacketSender(gr.Process)
	ctx.Logger = logger
	ctx.Manager = game.NewGameManager(gr, hidM, supervisorName)
	ctx.GameReader = gr
	ctx.MemoryInjector = gi
	ctx.PathFinder = pf
	ctx.BeltManager = bm
	ctx.HealthManager = hm
	char, err := character.BuildCharacter(ctx.Context)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating character: %w", err)
	}
	ctx.Char = char

	muleManager := mule.NewManager(logger)
	bot := NewBot(ctx.Context, muleManager)

	statsHandler := NewStatsHandler(supervisorName, logger)
	mng.eventListener.Register(statsHandler.Handle)
	supervisor, err := NewSinglePlayerSupervisor(supervisorName, bot, statsHandler)

	if err != nil {
		return nil, nil, err
	}

	supervisor.GetContext().StopSupervisorFn = supervisor.Stop

	// This function will be used to restart the client - passed to the crashDetector
	restartFunc := func() {

		ctx := supervisor.GetContext()
		if ctx.CleanStopRequested {
			if ctx.RestartWithCharacter != "" {
				mng.logger.Info("Supervisor requested restart with different character",
					slog.String("from", supervisorName),
					slog.String("to", ctx.RestartWithCharacter))
				nextCharacter := ctx.RestartWithCharacter
				mng.Stop(supervisorName)
				time.Sleep(5 * time.Second) // Wait before starting new character
				if err := mng.Start(nextCharacter, false); err != nil {
					mng.logger.Error("Failed to start next character",
						slog.String("character", nextCharacter),
						slog.String("error", err.Error()))
				}
				return
			}
			mng.logger.Info("Supervisor stopped cleanly by game logic. Preventing restart.", slog.String("supervisor", supervisorName))
			mng.Stop(supervisorName)
			return
		}

		mng.logger.Info("Restarting supervisor after crash", slog.String("supervisor", supervisorName))
		mng.Stop(supervisorName)
		time.Sleep(5 * time.Second) // Wait a bit before restarting

		// Get a list of all available Supervisors
		supervisorList := mng.AvailableSupervisors()

		for {

			// Set the default state
			tokenAuthStarting := false

			// Get the current supervisor's config
			supCfg, _ := config.GetCharacter(supervisorName)

			for _, sup := range supervisorList {

				// If the current don't check against the one we're trying to launch
				if sup == supervisorName {
					continue
				}

				if mng.GetSupervisorStats(sup).SupervisorStatus == Starting {
					if supCfg.AuthMethod == "TokenAuth" {
						tokenAuthStarting = true
						mng.logger.Info("Waiting before restart as another client is already starting and we're using token auth", slog.String("supervisor", sup))
						break
					}

					sCfg, found := config.GetCharacter(sup)
					if found {
						if sCfg.AuthMethod == "TokenAuth" {
							// A client that uses token auth is currently starting, hold off restart
							tokenAuthStarting = true
							mng.logger.Info("Waiting before restart as a client that's using token auth is already starting", slog.String("supervisor", sup))
							break
						}
					}
				}
			}

			if !tokenAuthStarting {
				break
			}

			// Wait 5 seconds before checking again
			utils.Sleep(5000)
		}

		gameTitle := "D2R - [" + strconv.FormatInt(int64(pid), 10) + "] - " + supervisorName + " - " + cfg.Realm
		winproc.SetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(gameTitle))))

		err := mng.Start(supervisorName, false)
		if err != nil {
			mng.logger.Error("Failed to restart supervisor", slog.String("supervisor", supervisorName), slog.String("Error: ", err.Error()))
		}
	}

	gameTitle := "D2R - [" + strconv.FormatInt(int64(pid), 10) + "] - " + supervisorName + " - " + cfg.Realm
	winproc.SetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(gameTitle))))
	crashDetector := game.NewCrashDetector(supervisorName, int32(pid), uintptr(hwnd), mng.logger, restartFunc)

	return supervisor, crashDetector, nil
}

func (mng *SupervisorManager) GetSupervisorStats(supervisor string) Stats {
	if mng.supervisors[supervisor] == nil {
		return Stats{}
	}
	return mng.supervisors[supervisor].Stats()
}

func (mng *SupervisorManager) rearrangeWindows() {
	width := win.GetSystemMetrics(0)
	height := win.GetSystemMetrics(1)
	var windowBorderX int32 = 2   // left + right window border is 2px
	var windowBorderY int32 = 40  // upper window border is usually 40px
	var windowOffsetX int32 = -10 // offset horizontal window placement by -10 pixel
	maxColumns := width / (1280 + windowBorderX)
	maxRows := height / (720 + windowBorderY)

	mng.logger.Debug(
		"Arranging windows",
		slog.String("displaywidth", strconv.FormatInt(int64(width), 10)),
		slog.String("displayheight", strconv.FormatInt(int64(height), 10)),
		slog.String("max columns", strconv.FormatInt(int64(maxColumns+1), 10)), // +1 as we are counting from 0
		slog.String("max rows", strconv.FormatInt(int64(maxRows+1), 10)),
	)

	var column, row int32
	for _, sp := range mng.supervisors {
		// reminder that columns are vertical (they go up and down) and rows are horizontal (they go left and right)
		if column > maxColumns {
			column = 0
			row++
		}

		if row <= maxRows {
			sp.SetWindowPosition(int(column*(1280+windowBorderX)+windowOffsetX), int(row*(720+windowBorderY)))
			mng.logger.Debug(
				"Window Positions",
				slog.String("supervisor", sp.Name()),
				slog.String("column", strconv.FormatInt(int64(column), 10)),
				slog.String("row", strconv.FormatInt(int64(row), 10)),
				slog.String("position", strconv.FormatInt(int64(column*(1280+windowBorderX)+windowOffsetX), 10)+"x"+strconv.FormatInt(int64(row*(720+windowBorderY)), 10)),
			)
			column++
		} else {
			mng.logger.Debug("Window position of supervisor " + sp.Name() + " was not changed, no free space for it")
		}
	}
}
