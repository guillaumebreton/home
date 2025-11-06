package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"text/template"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type Configuration struct {
	Links []Link `yaml:"links"`
}

type Link struct {
	Name string `yaml:"name"`
	Url  string `yaml:"url"`
}

type Handler struct {
	mu       sync.RWMutex
	config   Configuration
	template *template.Template
}

func NewHandler(config Configuration) (*Handler, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Handler{
		config:   config,
		template: tmpl,
	}, nil
}

func (h *Handler) getConfig() Configuration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

func (h *Handler) index(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	config := h.getConfig()
	// Execute the template by name
	if err := h.template.ExecuteTemplate(w, "links.html", config); err != nil {
		http.Error(w, fmt.Sprintf("Error rendering template: %v", err), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) updateConfig(config Configuration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config = config
	log.Printf("Configuration updated: %+v\n", config)
}

// LoadConfig loads configuration from file
func loadConfig(filename string) (Configuration, error) {
	f, err := os.ReadFile(filename)
	if err != nil {
		return Configuration{}, err
	}

	var config Configuration
	if err := yaml.Unmarshal(f, &config); err != nil {
		return Configuration{}, err
	}
	return config, nil
}

//go:embed templates/*
var templatesFS embed.FS

type AppConfig struct {
	ConfigFile string
	BindAddr   string
	BindPort   int
}

func parseFlags() AppConfig {
	var appConfig AppConfig

	flag.StringVar(&appConfig.ConfigFile, "config", "config.yaml", "Path to configuration file")
	flag.StringVar(&appConfig.ConfigFile, "c", "config.yaml", "Path to configuration file (shorthand)")

	flag.StringVar(&appConfig.BindAddr, "bind-addr", "0.0.0.0", "Bind address for the server")
	flag.StringVar(&appConfig.BindAddr, "a", "0.0.0.0", "Bind address for the server (shorthand)")

	flag.IntVar(&appConfig.BindPort, "port", 8080, "Port to bind the server")
	flag.IntVar(&appConfig.BindPort, "p", 8080, "Port to bind the server (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A simple link manager with auto-reloading configuration.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -c ./myconfig.yaml -p 3000\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --config=/etc/links/config.yaml --bind-addr=127.0.0.1 --port=9090\n", os.Args[0])
	}

	flag.Parse()

	return appConfig
}

func watchConfig(configPath string, handler *Handler) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Watch the directory, not the file (Kubernetes uses symlinks)
	configDir := filepath.Dir(configPath)
	err = watcher.Add(configDir)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Kubernetes updates ConfigMaps by updating symlinks
			if event.Op&fsnotify.Create == fsnotify.Create ||
				event.Op&fsnotify.Write == fsnotify.Write {
				log.Println("Config file changed, reloading...")
				config, err := loadConfig(configPath)
				if err != nil {
					log.Printf("Error reloading config: %v", err)
				}
				handler.updateConfig(config)

			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

func main() {

	// Parse command-line flags
	appConfig := parseFlags()

	// Display configuration
	log.Printf("Starting with configuration:")
	log.Printf("  Config file: %s", appConfig.ConfigFile)
	log.Printf("  Bind address: %s", appConfig.BindAddr)
	log.Printf("  Port: %d", appConfig.BindPort)

	// Check if config file exists
	if _, err := os.Stat(appConfig.ConfigFile); os.IsNotExist(err) {
		log.Fatalf("Configuration file not found: %s", appConfig.ConfigFile)
	}
	config, err := loadConfig(appConfig.ConfigFile)
	if err != nil {
		log.Fatal(err)
	}

	handler, err := NewHandler(config)
	if err != nil {
		log.Fatal(err)
	}

	go watchConfig(appConfig.ConfigFile, handler)

	log.Println("Server starting on :8080")
	http.HandleFunc("/", handler.index)
	bindAddress := fmt.Sprintf("%s:%d", appConfig.BindAddr, appConfig.BindPort)
	if err := http.ListenAndServe(bindAddress, nil); err != nil {
		log.Fatal(err)
	}
}
