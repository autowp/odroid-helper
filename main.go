package main

import (
	"context"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const Namespace = "odroid"
const Name = "temperature_celsius"

var (
	addr              = flag.String("addr", ":9590", "The address to listen on for HTTP requests.")
	interval          = flag.Duration("interval", 10*time.Second, "The interval at which the temperature is checked.")
	verbose           = flag.Bool("debug", false, "Enable debug output.")
	fanInterval       = flag.Duration("fan-interval", 2*time.Second, "The interval at which the fan is adjusted.")
	startFanThreshold = flag.Float64("start-fan-threshold", 75, "Start fan threshold")
	stopFanThreshold  = flag.Float64("stop-fan-threshold", 45, "Stop fan threshold")
	startFanCmd       = flag.String("start-fan-cmd", "i2cset -y 1 0x60 0x05 0x00", "Start fan cmd.")
	stopFanCmd        = flag.String("stop-fan-cmd", "i2cset -y 1 0x60 0x05 0x05", "Stop fan cmd.")
)

func main() {

	log.SetFlags(0)
	ctx := Context()
	flag.Parse()

	s := Server(*addr)
	var zone0 = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   Namespace,
		Name:        Name,
		Help:        "Temperature",
		ConstLabels: map[string]string{"zone": "zone0"},
	})
	var zone1 = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   Namespace,
		Name:        Name,
		Help:        "Temperature",
		ConstLabels: map[string]string{"zone": "zone1"},
	})
	var zone2 = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   Namespace,
		Name:        Name,
		Help:        "Temperature",
		ConstLabels: map[string]string{"zone": "zone2"},
	})
	var zone3 = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace:   Namespace,
		Name:        Name,
		Help:        "Temperature",
		ConstLabels: map[string]string{"zone": "zone3"},
	})
	prometheus.MustRegister(zone0, zone1, zone2, zone3)

	debug := log.New(ioutil.Discard, "", 0)
	if *verbose {
		debug.SetOutput(os.Stderr)
	} else {
		log.Printf("Starting pi-temp web server at %q", *addr)
		log.Println("If you want to see more verbose log run with -debug")
	}

	go monitorTemperature(ctx, debug, "/sys/class/thermal/thermal_zone0/temp", *interval, zone0)
	go monitorTemperature(ctx, debug, "/sys/class/thermal/thermal_zone1/temp", *interval, zone1)
	go monitorTemperature(ctx, debug, "/sys/class/thermal/thermal_zone2/temp", *interval, zone2)
	go monitorTemperature(ctx, debug, "/sys/class/thermal/thermal_zone3/temp", *interval, zone3)
	go func() { log.Fatal(s.ListenAndServe()) }()
	go fanControl(ctx, debug, "/sys/class/thermal/thermal_zone0/temp", *fanInterval)

	// Run until we are interrupted
	<-ctx.Done()
	s.Shutdown(Context())

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	for sig := range c {
		log.Printf("captured %v, stopping and exiting.", sig)

		os.Exit(0)
	}

	os.Exit(0)
}

// Context returns a context that is cancelled automatically when a SIGINT,
// SIGQUIT or SIGTERM signal is received.
func Context() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			cancel()
		}
	}()

	return ctx
}

// Server creates the HTTP server that is used by Prometheus to scrape the temperature metric.
func Server(addr string) *http.Server {
	h := http.NewServeMux()
	h.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorLog:     log.New(os.Stderr, "HTTP: ", 0),
	}
}

func getZoneTemperature(path string) float64 {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read temperature from %q: %v", path, err)
	}

	cpuTempStr := strings.TrimSpace(string(raw))
	cpuTempInt, err := strconv.Atoi(cpuTempStr)
	if err != nil {
		log.Fatalf("%q does not contain an integer: %v", path, err)
	}

	cpuTemp := float64(cpuTempInt) / 1000.0

	return cpuTemp
}

func monitorTemperature(ctx context.Context, debug *log.Logger, path string, interval time.Duration, temperature prometheus.Gauge) {
	debug.Printf("Checking temperature every %v from %q", interval, path)

	for {

		cpuTemp := getZoneTemperature(path)

		debug.Printf("CPU temperature: %.3f°C", cpuTemp)
		temperature.Set(cpuTemp)

		select {
		case <-time.After(interval):
			continue
		case <-ctx.Done():
			return
		}
	}
}

func fanControl(ctx context.Context, debug *log.Logger, path string, interval time.Duration) {
	for {
		cpuTemp := getZoneTemperature(path)

		if cpuTemp > *startFanThreshold {
			debug.Printf("Starting fan at %.1f°", cpuTemp)
			cmd := exec.Command("bash", "-c", *startFanCmd)
			err := cmd.Run()
			if err != nil {
				log.Fatalf("Failed top start fan: %s", err.Error())
			}

		} else if cpuTemp < *stopFanThreshold {
			debug.Printf("Stopping fan at %.1f°", cpuTemp)
			cmd := exec.Command("bash", "-c", *stopFanCmd)
			err := cmd.Run()
			if err != nil {
				log.Fatalf("Failed to stop fan: %s", err.Error())
			}
		}

		select {
		case <-time.After(interval):
			continue
		case <-ctx.Done():
			return
		}
	}
}
