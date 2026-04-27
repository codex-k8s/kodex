package app

import "os"

type Config struct {
	HTTPAddr string
	GRPCAddr string
}

func LoadConfigFromEnv() Config {
	return Config{
		HTTPAddr: envOrDefault("KODEX_ACCESS_MANAGER_HTTP_ADDR", ":8080"),
		GRPCAddr: envOrDefault("KODEX_ACCESS_MANAGER_GRPC_ADDR", ":9090"),
	}
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
