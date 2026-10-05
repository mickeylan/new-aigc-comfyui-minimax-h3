package main

import (
	"log"

	"comfyui-console/internal/api"
	"comfyui-console/internal/config"
	"comfyui-console/internal/database"
	"comfyui-console/internal/service"
)

func main() {
	cfg := config.Load()

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
