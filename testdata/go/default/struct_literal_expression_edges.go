package sample

type Config struct {
	Name string
	Port int
}

func buildConfigs(port int) []Config {
	defaults := []Config{
		{Name: "api", Port: port},
		{Name: "admin", Port: port + 1},
	}
	anon := struct {
		Label string
		Port  int
	}{Label: "edge", Port: port}
	anonPtr := &struct {
		Label string
		Port  int
	}{Label: anon.Label, Port: anon.Port}
	typed := []struct {
		Primary Config
	}{
		{Primary: Config{Name: defaults[0].Name, Port: defaults[0].Port}},
	}
	_ = anonPtr
	return []Config{typed[0].Primary}
}
