package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func isPocketbaseInstanceActive(portNumber int) (bool, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/health", portNumber)
	cmd := exec.Command("curl", "-s", url)
	output, err := cmd.Output()

	if err != nil {
		return false, err
	}
	re := regexp.MustCompile(`"code"\s*:\s*([^,]+)`)
	matches := re.FindStringSubmatch(string(output))
	match := matches[0]
	isActive := strings.Contains(match, "200")

	return isActive, nil
}

func servePocketbase(portNumber int, dirName string) (*int, error) {
	if err := os.MkdirAll(dirName, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	logFile := filepath.Join(dirName, fmt.Sprintf("pocketbase-%s.log", dirName))
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)

	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Prepare the command
	cmd := exec.Command(
		"./pocketbase", "serve",
		fmt.Sprintf("--dir=%s/pb_data", dirName),
		fmt.Sprintf("--publicDir=%s/pb_public", dirName),
		fmt.Sprintf("--hooksDir=%s/pb_hooks", dirName),
		fmt.Sprintf("--migrationsDir=%s/pb_migrations", dirName),
		fmt.Sprintf("--http=127.0.0.1:%d", portNumber),
	)

	// Redirect stdout and stderr to log file
	cmd.Stdout = f
	cmd.Stderr = f

	// Start the process asynchronously
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start PocketBase: %w", err)
	}

	// Return PID pointer
	pid := cmd.Process.Pid
	return &pid, nil
}

func main() {
	app := pocketbase.New()

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// serves static files from the provided public dir (if exists)
		se.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))

		return se.Next()
	})

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		fmt.Println("onBootstrap")

		err := e.Next()

		return err
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		fmt.Println("onServe")

		records, err := app.FindAllRecords("instances")
		if err != nil {
			return e.Next()
		}

		for _, record := range records {
			portNumber := record.GetInt("portNumber")
			dirName := record.GetString("dirName")
			pid, _ := servePocketbase(portNumber, dirName)
			record.Set("pid", pid)
			app.Save(record)
		}

		for _, record := range records {
			portNumber := record.GetInt("portNumber")
			isActive, _ := isPocketbaseInstanceActive(portNumber)
			record.Set("isActive", isActive)
			app.Save(record)
		}

		return e.Next()
	})

	app.OnRecordAfterCreateSuccess("instances").BindFunc(func(e *core.RecordEvent) error {
		fmt.Println("onRecordAfterCreateSuccess")

		portNumber := e.Record.GetInt("portNumber")
		dirName := e.Record.GetString("dirName")

		pid, err := servePocketbase(portNumber, dirName)
		if err != nil {
			return e.Next()
		}

		isActive, _ := isPocketbaseInstanceActive(portNumber)

		e.Record.Set("pid", pid)
		e.Record.Set("isActive", isActive)

		err = app.Save(e.Record)

		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
