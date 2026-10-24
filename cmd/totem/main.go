// Comando totem: server dei documenti di sicurezza.
//
//	totem                 avvia il server (equivale a "totem serve")
//	totem healthcheck     usato da HEALTHCHECK nel Dockerfile
//	totem reset-pairing   invalida i token dei dispositivi e genera un nuovo codice di setup
//	                      (--reset-pin azzera anche il PIN)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // TZ=Europe/Rome anche su distroless

	"github.com/toigoweb/totem/internal/api"
	"github.com/toigoweb/totem/internal/config"
	"github.com/toigoweb/totem/internal/scanner"
	"github.com/toigoweb/totem/internal/synclog"
	"github.com/toigoweb/totem/internal/version"
	"github.com/toigoweb/totem/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	dataDir      = env("TOTEM_DATA", "/data")
	secretsDir   = env("TOTEM_SECRETS", "/secrets")
	listenAddr   = env("TOTEM_ADDR", ":8080")
	internalAddr = env("TOTEM_INTERNAL_ADDR", "127.0.0.1:8081")
)

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		os.Exit(serve())
	case "healthcheck":
		os.Exit(healthcheck())
	case "reset-pairing":
		os.Exit(resetPairing(args))
	case "version", "--version", "-v":
		fmt.Println(version.Version)
	default:
		fmt.Fprintf(os.Stderr, "comando sconosciuto %q (serve | healthcheck | reset-pairing [--reset-pin] | version)\n", cmd)
		os.Exit(2)
	}
}

func serve() int {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("avvio totem", "versione", version.Version, "dati", dataDir)
	for _, d := range []string{dataDir, secretsDir} {
		if err := checkWritable(d); err != nil {
			logger.Error("cartella non scrivibile: sul server eseguire 'sudo chown -R 65532:65532 ./data ./secrets'", "cartella", d, "err", err)
			return 1
		}
	}

	store, err := config.Open(filepath.Join(dataDir, "config.json"), filepath.Join(dataDir, "backup"))
	if err != nil {
		logger.Error("configurazione", "err", err)
		return 1
	}
	ci, created, err := config.LoadOrCreateKey(filepath.Join(secretsDir, "key"))
	if err != nil {
		logger.Error("chiave di cifratura", "err", err)
		return 1
	}
	if created {
		withPwd := 0
		for _, sec := range store.Get().Sezioni {
			if sec.Share.Password != "" {
				withPwd++
			}
		}
		if withPwd > 0 {
			logger.Warn("generata una nuova chiave in /secrets/key: le password SMB salvate non sono più leggibili, reinserirle dal totem", "sezioni", withPwd)
		} else {
			logger.Info("generata la chiave di cifratura in /secrets/key")
		}
	}
	settings, warns := config.ResolveSettings(store.Get().Impostazioni)
	for _, w := range warns {
		logger.Warn(w)
	}
	syncLog, err := synclog.Open(filepath.Join(dataDir, "logs", "sync.log"), 5<<20, 5)
	if err != nil {
		logger.Error("log di sync", "err", err)
		return 1
	}
	// TOTEM_SMB_PORT serve solo ai test (Samba su una porta diversa dalla 445)
	smbPort, _ := strconv.Atoi(os.Getenv("TOTEM_SMB_PORT"))
	sc, err := scanner.New(scanner.Options{DataDir: dataDir, Config: store, Cipher: ci, Settings: settings, Log: syncLog, Logger: logger, Port: smbPort})
	if err != nil {
		logger.Error("scanner", "err", err)
		return 1
	}
	// nuova scansione solo se cambiano le sezioni (non per gli heartbeat)
	lastSections := sectionsKey(store.Get())
	store.OnChange(func() {
		if k := sectionsKey(store.Get()); k != lastSections {
			lastSections = k
			sc.Kick()
		}
	})

	srv := api.New(api.Options{Config: store, Cipher: ci, Scanner: sc, Log: syncLog, Settings: settings, DataDir: dataDir,
		Static: web.Dist(), Version: version.Version, Logger: logger})
	srv.EnsureSetupCode()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go sc.Run(ctx)

	pub := &http.Server{Addr: listenAddr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	in := &http.Server{Addr: internalAddr, Handler: srv.InternalHandler(), ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 2)
	go func() { errc <- pub.ListenAndServe() }()
	go func() { errc <- in.ListenAndServe() }()
	logger.Info("in ascolto", "http", listenAddr, "interno", internalAddr)
	select {
	case <-ctx.Done():
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http", "err", err)
			return 1
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = pub.Shutdown(sctx)
	_ = in.Shutdown(sctx)
	logger.Info("arresto completato")
	return 0
}

func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".scrivibile-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

func sectionsKey(c *config.Config) string {
	b, _ := json.Marshal(c.Sezioni)
	return string(b)
}

func internalURL(p string) string {
	host, port, err := net.SplitHostPort(internalAddr)
	if err != nil || host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + p
}

func healthcheck() int {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(internalURL("/internal/health"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "non in salute:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "non in salute: HTTP", resp.StatusCode)
		return 1
	}
	return 0
}

func resetPairing(args []string) int {
	fs := flag.NewFlagSet("reset-pairing", flag.ContinueOnError)
	resetPin := fs.Bool("reset-pin", false, "azzera anche il PIN di manutenzione")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	q := ""
	if *resetPin {
		q = "?reset-pin=1"
	}
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Post(internalURL("/internal/reset-pairing"+q), "application/json", bytes.NewReader(nil))
	if err == nil {
		defer resp.Body.Close()
		var out struct {
			SetupCode string `json:"setupCode"`
		}
		if resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&out) == nil {
			fmt.Printf("Abbinamenti azzerati%s.\nNuovo codice di setup: %s\n", pinNote(*resetPin), out.SetupCode)
			return 0
		}
		fmt.Fprintln(os.Stderr, "reset-pairing: risposta inattesa dal server, HTTP", resp.StatusCode)
		return 1
	}
	// server spento: si modifica direttamente config.json; il codice comparirà nei log al prossimo avvio
	store, err := config.Open(filepath.Join(dataDir, "config.json"), filepath.Join(dataDir, "backup"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset-pairing:", err)
		return 1
	}
	if err := store.Update(func(c *config.Config) error {
		c.Globali.Dispositivi = []config.Device{}
		if *resetPin {
			c.Globali.PinHash = ""
		}
		return nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "reset-pairing:", err)
		return 1
	}
	fmt.Printf("Server non in esecuzione: abbinamenti azzerati%s.\nIl codice di setup comparirà nei log al prossimo avvio (docker compose logs totem).\n", pinNote(*resetPin))
	return 0
}

func pinNote(reset bool) string {
	if reset {
		return " e PIN azzerato"
	}
	return " (PIN e sezioni invariati)"
}
