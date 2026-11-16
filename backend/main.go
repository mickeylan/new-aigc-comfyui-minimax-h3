package main

import (
	"log"
	"time"

	"comfyui-console/internal/api"
	"comfyui-console/internal/applog"
	"comfyui-console/internal/config"
	"comfyui-console/internal/database"
	"comfyui-console/internal/service"
)

func main() {
	cfg := config.Load()

	if logFile, logPath, err := applog.Setup(cfg.Storage.DataDir, time.Now()); err != nil {
		log.Printf("[log] file log disabled, console only: %v", err)
	} else {
		defer logFile.Close()
		log.Printf("[log] writing console log to %s", logPath)
	}

	db, err := database.Init(cfg)
	if err != nil {
		log.Fatalf("init database failed: %v", err)
	}

	svc := service.New(cfg, db)
	if err := svc.Start(); err != nil {
		log.Fatalf("start service failed: %v", err)
	}
	defer svc.Stop()

	r := api.NewRouter(cfg, svc)

	log.Printf("ComfyUI Console listening on %s", cfg.Server.Addr)
	if err := r.Run(cfg.Server.Addr); err != nil {
		log.Fatalf("server exit: %v", err)
	}
}
