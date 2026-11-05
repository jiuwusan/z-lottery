package main

import "testing"

func TestConfigurationDefaultsAndInvalidValues(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "3000")
	t.Setenv("DATABASE_PATH", ":memory:")
	t.Setenv("CHECK_ENABLED", "true")
	t.Setenv("CHECK_SCHEDULE", "*/20 22-23 * * *;0 0 * * *")
	t.Setenv("CHECK_TIMEZONE", "Asia/Shanghai")
	c, e := loadConfig()
	if e != nil || !c.enabled || c.zone != "Asia/Shanghai" {
		t.Fatal(c, e)
	}
	for _, value := range []string{"0", "65536", "x"} {
		t.Setenv("PORT", value)
		if _, e = loadConfig(); e == nil {
			t.Fatal("invalid port accepted", value)
		}
	}
	t.Setenv("PORT", "3000")
	t.Setenv("CHECK_ENABLED", "maybe")
	if _, e = loadConfig(); e == nil {
		t.Fatal("invalid flag accepted")
	}
	t.Setenv("CHECK_ENABLED", "false")
	c, e = loadConfig()
	if e != nil || c.enabled {
		t.Fatal(c, e)
	}
	t.Setenv("DATABASE_PATH", "")
	if _, e = loadConfig(); e == nil {
		t.Fatal("empty database accepted")
	}
}
