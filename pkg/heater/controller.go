package heater

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultGPIO = 17
)

type Config struct {
	GPIO             int
	OnBelowC         float64
	OffAboveC        float64
	ActiveHigh       *bool
	UnexportOnClose  bool
	ExportRetryCount int
}

type Controller struct {
	cfg           Config
	gpio          *sysfsGPIO
	mu            sync.Mutex
	on            bool
	mode          string
	manualOn      bool
	lastMinTemp   *float64
	lastTemps     []float64
	lastUpdatedAt time.Time
}

func NewController(cfg Config) (*Controller, error) {
	applyDefaults(&cfg)
	if cfg.OffAboveC < cfg.OnBelowC {
		return nil, fmt.Errorf("heater off threshold %.2fC must be >= on threshold %.2fC", cfg.OffAboveC, cfg.OnBelowC)
	}

	gpio, err := newSysfsGPIO(cfg.GPIO, derefBool(cfg.ActiveHigh, true), cfg.ExportRetryCount)
	if err != nil {
		return nil, err
	}

	return &Controller{
		cfg:  cfg,
		gpio: gpio,
		mode: "auto",
	}, nil
}

func (c *Controller) UpdateTemps(temps []float64) {
	if len(temps) == 0 {
		return
	}
	minTemp := temps[0]
	for i := 1; i < len(temps); i++ {
		if temps[i] < minTemp {
			minTemp = temps[i]
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	cachedTemps := make([]float64, len(temps))
	copy(cachedTemps, temps)
	c.lastTemps = cachedTemps
	c.lastMinTemp = &minTemp
	c.lastUpdatedAt = time.Now().UTC()

	if c.mode == "manual" {
		return
	}

	desired := c.on
	if c.on {
		if minTemp >= c.cfg.OffAboveC {
			desired = false
		}
	} else {
		if minTemp <= c.cfg.OnBelowC {
			desired = true
		}
	}
	if desired == c.on {
		return
	}

	if err := c.gpio.Set(desired); err != nil {
		log.Println("heater gpio set failed:", err)
		return
	}
	c.on = desired
	if desired {
		log.Printf("Heater ON (mode=auto, min temp %.1fC)\n", minTemp)
	} else {
		log.Printf("Heater OFF (mode=auto, min temp %.1fC)\n", minTemp)
	}
}

func (c *Controller) SetMode(mode string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch mode {
	case "auto":
		if c.mode != "auto" {
			log.Println("Heater mode set to auto")
		}
		c.mode = "auto"
		return nil
	case "manual":
		if c.mode != "manual" {
			log.Println("Heater mode set to manual")
		}
		c.mode = "manual"
		if err := c.gpio.Set(c.manualOn); err != nil {
			return err
		}
		c.on = c.manualOn
		log.Printf("Heater %s (mode=manual)\n", onOffLabel(c.on))
		return nil
	default:
		return fmt.Errorf("unsupported heater mode %q", mode)
	}
}

func (c *Controller) SetManualOn(on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manualOn = on
	if c.mode != "manual" {
		return nil
	}
	if err := c.gpio.Set(on); err != nil {
		return err
	}
	c.on = on
	log.Printf("Heater %s (mode=manual)\n", onOffLabel(c.on))
	return nil
}

type Status struct {
	Mode         string     `json:"mode"`
	ManualOn     bool       `json:"manual_on"`
	On           bool       `json:"on"`
	MinTempC     *float64   `json:"min_temp_c,omitempty"`
	TempsC       []float64  `json:"temps_c,omitempty"`
	LastUpdateAt *time.Time `json:"last_update_at,omitempty"`
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	var lastUpdate *time.Time
	if !c.lastUpdatedAt.IsZero() {
		val := c.lastUpdatedAt
		lastUpdate = &val
	}
	var minTemp *float64
	if c.lastMinTemp != nil {
		val := *c.lastMinTemp
		minTemp = &val
	}
	tempsCopy := make([]float64, len(c.lastTemps))
	copy(tempsCopy, c.lastTemps)
	return Status{
		Mode:         c.mode,
		ManualOn:     c.manualOn,
		On:           c.on,
		MinTempC:     minTemp,
		TempsC:       tempsCopy,
		LastUpdateAt: lastUpdate,
	}
}

func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.gpio.Set(false)
	if c.cfg.UnexportOnClose {
		_ = c.gpio.Unexport()
	}
}

func applyDefaults(cfg *Config) {
	if cfg.GPIO == 0 {
		cfg.GPIO = defaultGPIO
	}
	if cfg.ExportRetryCount == 0 {
		cfg.ExportRetryCount = 5
	}
}

type sysfsGPIO struct {
	pin        int
	valuePath  string
	activeHigh bool
}

func newSysfsGPIO(pin int, activeHigh bool, retries int) (*sysfsGPIO, error) {
	base := "/sys/class/gpio"
	if _, err := os.Stat(base); err != nil {
		return nil, fmt.Errorf("gpio sysfs not available: %w", err)
	}

	gpioPath := filepath.Join(base, fmt.Sprintf("gpio%d", pin))
	if _, err := os.Stat(gpioPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		var resolvedPin int
		var resolvedPath string
		var err error
		resolvedPin, resolvedPath, err = exportGPIO(base, pin, retries)
		if err != nil {
			return nil, err
		}
		pin = resolvedPin
		gpioPath = resolvedPath
	}

	if err := os.WriteFile(filepath.Join(gpioPath, "direction"), []byte("out"), 0o644); err != nil {
		return nil, err
	}

	g := &sysfsGPIO{
		pin:        pin,
		valuePath:  filepath.Join(gpioPath, "value"),
		activeHigh: activeHigh,
	}
	if err := g.Set(false); err != nil {
		return nil, err
	}
	log.Printf("heater gpio: using value path %s (activeHigh=%v)\n", g.valuePath, g.activeHigh)
	return g, nil
}

func (g *sysfsGPIO) Set(on bool) error {
	val := "0"
	if on == g.activeHigh {
		val = "1"
	}
	return os.WriteFile(g.valuePath, []byte(val), 0o644)
}

func (g *sysfsGPIO) Unexport() error {
	return os.WriteFile("/sys/class/gpio/unexport", []byte(strconv.Itoa(g.pin)), 0o644)
}

func waitForGPIOPath(path string, retries int) {
	for i := 0; i < retries; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func derefBool(val *bool, fallback bool) bool {
	if val == nil {
		return fallback
	}
	return *val
}

func onOffLabel(on bool) string {
	if on {
		return "ON"
	}
	return "OFF"
}

func exportGPIO(base string, pin int, retries int) (int, string, error) {
	exportPath := filepath.Join(base, "export")
	gpioPath := filepath.Join(base, fmt.Sprintf("gpio%d", pin))
	err := os.WriteFile(exportPath, []byte(strconv.Itoa(pin)), 0o644)
	if err == nil || errors.Is(err, syscall.EBUSY) {
		waitForGPIOPath(gpioPath, retries)
		if _, statErr := os.Stat(gpioPath); statErr != nil {
			return pin, gpioPath, statErr
		}
		return pin, gpioPath, nil
	}
	if errors.Is(err, syscall.EINVAL) {
		mappedPin, label, mapErr := resolveSysfsGPIOPin(base, pin)
		if mapErr != nil {
			return pin, gpioPath, fmt.Errorf("export gpio %d failed: %w", pin, err)
		}
		mappedPath := filepath.Join(base, fmt.Sprintf("gpio%d", mappedPin))
		mapWriteErr := os.WriteFile(exportPath, []byte(strconv.Itoa(mappedPin)), 0o644)
		if mapWriteErr != nil && !errors.Is(mapWriteErr, syscall.EBUSY) {
			return pin, gpioPath, fmt.Errorf("export gpio %d failed: %w", mappedPin, mapWriteErr)
		}
		waitForGPIOPath(mappedPath, retries)
		if _, statErr := os.Stat(mappedPath); statErr != nil {
			return pin, gpioPath, statErr
		}
		log.Printf("heater gpio: mapped BCM %d to gpio %d (%s)\n", pin, mappedPin, label)
		return mappedPin, mappedPath, nil
	}
	return pin, gpioPath, fmt.Errorf("export gpio %d failed: %w", pin, err)
}

func resolveSysfsGPIOPin(base string, bcmPin int) (int, string, error) {
	chips, err := filepath.Glob(filepath.Join(base, "gpiochip*"))
	if err != nil {
		return 0, "", err
	}
	bestScore := -1
	bestPin := 0
	bestLabel := ""
	for _, chip := range chips {
		label := readGPIOChipString(filepath.Join(chip, "label"))
		baseVal, baseOK := readGPIOChipInt(filepath.Join(chip, "base"))
		ngpio, ngpioOK := readGPIOChipInt(filepath.Join(chip, "ngpio"))
		if !baseOK || !ngpioOK {
			continue
		}
		if bcmPin >= ngpio {
			continue
		}
		score := 1
		labelLower := strings.ToLower(label)
		if strings.Contains(labelLower, "bcm") || strings.Contains(labelLower, "raspberry") || strings.Contains(labelLower, "pinctrl") {
			score = 2
		}
		if score > bestScore {
			bestScore = score
			bestPin = baseVal + bcmPin
			bestLabel = label
		}
	}
	if bestScore < 0 {
		return 0, "", fmt.Errorf("no gpiochip found for bcm pin %d", bcmPin)
	}
	return bestPin, bestLabel, nil
}

func readGPIOChipInt(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, false
	}
	return val, true
}

func readGPIOChipString(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
