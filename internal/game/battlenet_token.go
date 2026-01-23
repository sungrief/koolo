package game

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// GetBattleNetToken logs in to Battle.net and returns the authentication token.
func GetBattleNetToken(username, password, realm string) (string, error) {
	return getBattleNetToken(username, password, realm, nil)
}

func GetBattleNetTokenWithDebug(username, password, realm string, debug func(string)) (string, error) {
	return getBattleNetToken(username, password, realm, debug)
}

func getBattleNetToken(username, password, realm string, debug func(string)) (string, error) {
	logLine := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fmt.Print(line)
		if debug != nil {
			debug(strings.TrimSuffix(line, "\n"))
		}
	}

	l := launcher.New().Headless(true).MustLaunch()
	browser := rod.New().ControlURL(l).MustConnect()
	defer browser.MustClose()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	page := browser.Context(ctx).MustPage()

	loginURL := getBattleNetLoginURL(realm)
	logLine("[DEBUG] Login URL: %s\n", loginURL)

	page.Navigate(loginURL)
	page.MustWaitLoad()
	logLine("[DEBUG] Login page loaded\n")

	emailInput, err := page.Timeout(10 * time.Second).Element("input[type='text']")
	if err != nil {
		return "", fmt.Errorf("failed to find email input field: %w", err)
	}
	emailInput.Input(username)
	logLine("[DEBUG] Username entered\n")

	continueBtn, err := page.Timeout(5 * time.Second).Element("button[type='submit']")
	if err != nil {
		return "", fmt.Errorf("failed to find continue button: %w", err)
	}
	continueBtn.Click(proto.InputMouseButtonLeft, 1)
	logLine("[DEBUG] Continue button clicked\n")

	time.Sleep(2 * time.Second)

	passwordInput, err := page.Timeout(10 * time.Second).Element("input[type='password']")
	if err != nil {
		return "", fmt.Errorf("failed to find password input field: %w", err)
	}
	passwordInput.Input(password)
	logLine("[DEBUG] Password entered\n")

	loginBtn, err := page.Timeout(5 * time.Second).Element("button[type='submit']")
	if err != nil {
		return "", fmt.Errorf("failed to find login button: %w", err)
	}
	loginBtn.Click(proto.InputMouseButtonLeft, 1)
	logLine("[DEBUG] Login button clicked\n")

	time.Sleep(3 * time.Second)
	logLine("[DEBUG] Starting to monitor URL for token...\n")

	maxAttempts := 15
	for i := 0; i < maxAttempts; i++ {
		currentURL := page.MustInfo().URL
		logLine("[DEBUG %d/%d] Current URL: %s\n", i+1, maxAttempts, currentURL)

		if strings.Contains(currentURL, "/challenge/") {
			logLine("[INFO] Additional authentication required! Opening browser window...\n")
			browser.MustClose()

			return getBattleNetTokenWithUI(username, password, realm, debug)
		}

		if strings.Contains(currentURL, "ST=") {
			parsedURL, err := url.Parse(currentURL)
			if err == nil {
				token := parsedURL.Query().Get("ST")
				if token != "" {
					logLine("[DEBUG] Token found\n")
					return token, nil
				}
			}
		}

		time.Sleep(1 * time.Second)
	}

	return "", errors.New("authentication token not found")
}

func getBattleNetTokenWithUI(username, password, realm string, debug func(string)) (string, error) {
	logLine := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fmt.Print(line)
		if debug != nil {
			debug(strings.TrimSuffix(line, "\n"))
		}
	}

	logLine("[INFO] Please complete additional authentication in the browser window...\n")

	l := launcher.New().Headless(false).MustLaunch()
	browser := rod.New().ControlURL(l).MustConnect()
	defer browser.MustClose()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	page := browser.Context(ctx).MustPage()

	loginURL := getBattleNetLoginURL(realm)
	page.Navigate(loginURL)
	page.MustWaitLoad()

	emailInput, err := page.Timeout(10 * time.Second).Element("input[type='text']")
	if err == nil {
		emailInput.Input(username)
		continueBtn, _ := page.Timeout(5 * time.Second).Element("button[type='submit']")
		if continueBtn != nil {
			continueBtn.Click(proto.InputMouseButtonLeft, 1)
		}
	}

	time.Sleep(2 * time.Second)

	passwordInput, err := page.Timeout(10 * time.Second).Element("input[type='password']")
	if err == nil {
		passwordInput.Input(password)
		loginBtn, _ := page.Timeout(5 * time.Second).Element("button[type='submit']")
		if loginBtn != nil {
			loginBtn.Click(proto.InputMouseButtonLeft, 1)
		}
	}

	logLine("[INFO] Waiting for authentication completion (2 minutes timeout)...\n")
	logLine("[INFO] Please check your email or Battle.net app for verification code\n")

	maxAttempts := 120
	for i := 0; i < maxAttempts; i++ {
		currentURL := page.MustInfo().URL

		if i%10 == 0 {
			logLine("[INFO] Waiting for authentication... (%d/%d seconds)\n", i, maxAttempts)
		}

		if strings.Contains(currentURL, "ST=") {
			parsedURL, _ := url.Parse(currentURL)
			token := parsedURL.Query().Get("ST")
			if token != "" {
				logLine("[DEBUG] Token found\n")
				return token, nil
			}
		}

		time.Sleep(1 * time.Second)
	}

	return "", errors.New("authentication timeout (2 minutes)")
}

func getBattleNetLoginURL(realm string) string {
	switch realm {
	case "eu.actual.battle.net":
		return "https://eu.battle.net/login/en/?externalChallenge=login&app=OSI"
	case "kr.actual.battle.net":
		return "https://kr.battle.net/login/en/?externalChallenge=login&app=OSI"
	case "us.actual.battle.net":
		return "https://us.battle.net/login/en/?externalChallenge=login&app=OSI"
	default:
		// Default to US
		return "https://us.battle.net/login/en/?externalChallenge=login&app=OSI"
	}
}
