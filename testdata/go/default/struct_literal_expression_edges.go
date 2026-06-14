package sample

type Config struct {
	Name string
	Port int
}

func buildConfigs(port int) []Config {
	defaults := []Config{
		{Name: "api", Port: port},
		{Name: "admin"},
	}
	anon := struct {
		Label string
		Port  int
	}{Label: "edge", Port: port}
	anonPartial := struct {
		Label string
		Port  int
	}{Label: "edge"}
	anonPtr := &struct {
		Label string
		Port  int
	}{Label: anonPartial.Label}
	typed := []struct {
		Primary Config
	}{
		{Primary: Config{Name: defaults[0].Name, Port: defaults[0].Port}},
		{Primary: Config{Name: defaults[1].Name}},
	}
	_ = anon
	_ = anonPtr
	return []Config{typed[0].Primary, typed[1].Primary}
}
