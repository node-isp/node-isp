package setup

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/apex/log"
	"github.com/manifoldco/promptui"
	"gopkg.in/yaml.v3"

	"github.com/node-isp/node-isp/pkg/config"
	"github.com/node-isp/node-isp/pkg/licence"
)

func Run() error {

	fmt.Println("🚀 Setting up NodeISP configuration 🚀")

	fmt.Println("Config will be saved to `" + config.File + "`")

	validateLicenceId := func(input string) error {
		if len(input) != 34 {
			return fmt.Errorf("licence ID must be 34 characters long")
		}

		if input[:8] != "licence_" {
			return fmt.Errorf("licence ID must start with 'licence_'")
		}

		return nil
	}

	// nodeisp_01j0a2rg87rqnecm3q161hx3xq
	validateLicenceCode := func(input string) error {
		if len(input) != 34 {
			return fmt.Errorf("licence code must be 28 characters long")
		}

		if input[:8] != "nodeisp_" {
			return fmt.Errorf("licence code must start with 'nodeisp_'")
		}
		return nil
	}

	validateEmail := func(input string) error {
		if !strings.Contains(input, "@") {
			return fmt.Errorf("email must contain an '@'")
		}

		if !strings.Contains(input, ".") {
			return fmt.Errorf("email must contain a '.'")
		}

		return nil
	}

	templates := &promptui.PromptTemplates{
		Prompt:  "{{ . }} ",
		Valid:   "{{ . | green }} ",
		Invalid: "{{ . | red }} ",
		Success: "{{ . | bold }} ",
	}

	licenceIdPrompt := promptui.Prompt{
		Label:     "Enter your licence ID > ",
		Templates: templates,
		Validate:  validateLicenceId,
	}

	licenceCodePrompt := promptui.Prompt{
		Label:     "Enter your licence code > ",
		Mask:      '*',
		Templates: templates,
		Validate:  validateLicenceCode,
	}

	emailPrompt := promptui.Prompt{
		Label:     "Enter your email > ",
		Templates: templates,
		Validate:  validateEmail,
	}

	storageDirPrompt := promptui.Prompt{
		Label:     "Enter a storage directory > ",
		Default:   "/var/lib/node-isp/",
		Templates: templates,
	}

	licenceId, err := licenceIdPrompt.Run()
	if err != nil {
		return err
	}
	licenceCode, err := licenceCodePrompt.Run()
	if err != nil {
		return err
	}
	email, err := emailPrompt.Run()
	if err != nil {
		return err
	}

	fmt.Printf("Validating licence...\n")

	lic, err := licence.New(licenceId, licenceCode)
	if err != nil {
		return err
	}

	fmt.Println("Licence validated...")
	fmt.Printf("Licence ID: %s\n", licenceId)
	fmt.Printf("Licence Domain: %s\n", lic.Domain)

	// Ask the user for a storage directory
	storageDir, err := storageDirPrompt.Run()
	if err != nil {
		return err
	}

	logDirPrompt := promptui.Prompt{
		Label:     "Enter a log directory > ",
		Default:   "/var/log/node-isp/",
		Templates: templates,
	}

	logDir, _ := logDirPrompt.Run()

	os.MkdirAll(storageDir, 0755)
	os.MkdirAll(logDir, 0755)

	// Generate the server configuration, and show the user the configuration
	// before saving it to disk

	token := make([]byte, 32)
	_, _ = rand.Read(token)
	key := base64.StdEncoding.EncodeToString(token)

	// Generate a random database password
	_, _ = rand.Read(token)
	dbpass := base64.StdEncoding.EncodeToString(token)

	// Generate a random redis password
	_, _ = rand.Read(token)
	redispass := base64.StdEncoding.EncodeToString(token)

	cfg := &config.Config{
		Licence: &config.Licence{
			ID:  licenceId,
			Key: licenceCode,
		},
		HTTPServer: &config.HTTPServer{
			Domains: []string{lic.Domain},
			TLS: &config.TLS{
				Email: email,
			},
		},
		Storage: &config.Storage{
			Data: storageDir,
			Logs: logDir,
		},
		App: &config.App{
			Name: "NodeISP",
			Key:  fmt.Sprintf("base64:%s", key),
		},
		Database: &config.Database{
			Name:     "nodeisp",
			Password: dbpass,
		},
		Redis: &config.Redis{
			Password: redispass,
		},
		Services: &config.Services{
			GoogleMapsApiKey: "Get your own key :)",
		},
	}

	previewConfig := promptui.Prompt{
		Label:     "Preview the configuration? ",
		IsConfirm: true,
	}

	preview, _ := previewConfig.Run()

	b, _ := yaml.Marshal(cfg)
	if strings.ToLower(preview) == "y" {
		fmt.Println("Server configuration. This should be saved to " + config.File)
		// Encode the configuration to YAML

		fmt.Println("---\n" + string(b))
	}

	confirmPrompt := promptui.Prompt{
		Label:     fmt.Sprintf("Save this configuration to %s? ", config.File),
		IsConfirm: true,
	}

	result, _ := confirmPrompt.Run()

	if strings.ToLower(result) == "y" {
		// Get the directpry path and create if it doesn't exist
		filepath.Dir(config.File)
		if err := os.MkdirAll(filepath.Dir(config.File), 0755); err != nil {
			return err
		}

		f, err := os.Create(config.File)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		if err != nil {
			return err
		}

		fmt.Println("Configuration saved to " + config.File)
	}

	// If this is a systemd-based system, ask the user if they want to set up the service
	if _, err := os.Stat("/etc/systemd/system"); err == nil {
		// set up the service
		confirmPrompt = promptui.Prompt{
			Label:     fmt.Sprint("Run the system service and start Node ISP? "),
			IsConfirm: true,
		}

		result, _ = confirmPrompt.Run()

		if strings.ToLower(result) == "y" {
			setupService()

			fmt.Println("NodeISP setup complete 🚀🚀🚀")
			fmt.Println("NodeISP is now running as a service. You can access the admin interface at https://" + lic.Domain + "/admin")
			fmt.Printf("Your App Key is: '%s'. Store this in a safe place, if you lose it, your data will be gone forever \r\n", key)

			return nil
		}
	}

	fmt.Println("NodeISP setup complete 🚀🚀🚀")
	fmt.Println("NodeISP is not running as a service. You can start it by running `nodeispd`")

	return nil
}

//go:embed nodeisp.service
var nodeispdService []byte

func setupService() {
	// Write the service file
	err := os.WriteFile("/etc/systemd/system/nodeisp.service", nodeispdService, 0644)
	if err != nil {
		log.Fatalf("Failed to write service file: %s", err)
	}

	fmt.Println("Service file written to /etc/systemd/system/nodeisp.service")

	// Reload systemd
	err = exec.Command("systemctl", "daemon-reload").Run()
	if err != nil {
		log.Fatalf("Failed to reload systemd: %s", err)
	}

	fmt.Println("Systemd reloaded")

	// Enable the service
	err = exec.Command("systemctl", "enable", "nodeisp").Run()
	if err != nil {
		log.Fatalf("Failed to enable service: %s", err)
	}

	fmt.Println("Service enabled")

	// Start the service
	err = exec.Command("systemctl", "start", "nodeisp").Run()
	if err != nil {
		log.Fatalf("Failed to start service: %s", err)
	}

	fmt.Println("Service started")
}
