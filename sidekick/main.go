package main

import (
	"net/http"
	"os/exec"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type SpeakRequest struct {
	Text string `json:"text"`
}

func main() {
	e := echo.New()

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.POST("/notifications/speak", handleSpeak)

	e.Logger.Fatal(e.Start(":12345"))
}

func handleSpeak(c echo.Context) error {
	var req SpeakRequest
	if err := c.Bind(&req); err != nil {
		return c.String(http.StatusBadRequest, "Invalid JSON")
	}

	if req.Text == "" {
		return c.String(http.StatusBadRequest, "Text is required")
	}

	words := strings.Fields(req.Text)
	if len(words) > 50 {
		return c.String(http.StatusBadRequest, "Text must be 50 words or less")
	}

	go func() {
		exec.Command("afplay", "/System/Library/Sounds/Glass.aiff").Run()
	}()

	go func() {
		exec.Command("say", "-v", "Zoe (Premium)", req.Text).Run()
	}()

	return c.NoContent(http.StatusOK)
}
