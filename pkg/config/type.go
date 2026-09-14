package config

type App struct {
	Web WebConfig
}

type WebConfig struct {
	Mode string
	Host string
	Port int
}
