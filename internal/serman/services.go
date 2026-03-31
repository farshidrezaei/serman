package serman

type EnvField struct {
	Key   string
	Label string
}

type ServiceConfig struct {
	Name     string
	Env      []EnvField
	Volumes  []string
	HasTmpfs bool
}

var Services = []ServiceConfig{
	{
		Name: "redis",
		Env: []EnvField{
			{Key: "REDIS_PORT", Label: "Port"},
			{Key: "REDIS_PASSWORD", Label: "Password"},
		},
		Volumes: []string{"redis_data"},
	},
	{
		Name: "mysql",
		Env: []EnvField{
			{Key: "MYSQL_PORT", Label: "Port"},
			{Key: "MYSQL_DATABASE", Label: "Database"},
			{Key: "MYSQL_USER", Label: "User"},
			{Key: "MYSQL_PASSWORD", Label: "Password"},
			{Key: "MYSQL_ROOT_PASSWORD", Label: "Root Password"},
		},
		Volumes: []string{"mysql_data"},
	},
	{
		Name: "mysql_testing",
		Env: []EnvField{
			{Key: "MYSQL_TESTING_PORT", Label: "Port"},
			{Key: "MYSQL_TESTING_DATABASE", Label: "Database"},
			{Key: "MYSQL_TESTING_USER", Label: "User"},
			{Key: "MYSQL_TESTING_PASSWORD", Label: "Password"},
			{Key: "MYSQL_TESTING_ROOT_PASSWORD", Label: "Root Password"},
		},
		HasTmpfs: true,
	},
	{
		Name: "mongo",
		Env: []EnvField{
			{Key: "MONGODB_PORT", Label: "Port"},
			{Key: "MONGODB_DATABASE", Label: "Database"},
			{Key: "MONGODB_USERNAME", Label: "User"},
			{Key: "MONGODB_PASSWORD", Label: "Password"},
		},
		Volumes: []string{"mongo_data"},
	},
	{
		Name: "postgres",
		Env: []EnvField{
			{Key: "POSTGRES_PORT", Label: "Port"},
			{Key: "POSTGRES_DB", Label: "Database"},
			{Key: "POSTGRES_USER", Label: "User"},
			{Key: "POSTGRES_PASSWORD", Label: "Password"},
		},
		Volumes: []string{"postgres_data"},
	},
	{
		Name: "postgres_testing",
		Env: []EnvField{
			{Key: "POSTGRES_TESTING_PORT", Label: "Port"},
			{Key: "POSTGRES_TESTING_DB", Label: "Database"},
			{Key: "POSTGRES_TESTING_USER", Label: "User"},
			{Key: "POSTGRES_TESTING_PASSWORD", Label: "Password"},
		},
		HasTmpfs: true,
	},
}

func ServiceByName(name string) (ServiceConfig, bool) {
	for _, s := range Services {
		if s.Name == name {
			return s, true
		}
	}
	return ServiceConfig{}, false
}
