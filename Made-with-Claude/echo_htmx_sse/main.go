package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()

	// Serve the index.html file for the root path
	e.GET("/", func(c echo.Context) error {
		return c.File("index.html")
	})

	// Handle the SSE endpoint
	e.GET("/events", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
		c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
		c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
		c.Response().WriteHeader(http.StatusOK)

		randSource := rand.NewSource(time.Now().UnixNano())
		randGen := rand.New(randSource)

		for {
			randNum := randGen.Intn(100)
			event := fmt.Sprintf("data: Random number suka: %d\n\n", randNum)
			_, err := c.Response().Write([]byte(event))
			if err != nil {
				return err
			}
			c.Response().Flush()
			time.Sleep(1 * time.Second)
		}
	})

	e.Logger.Fatal(e.Start(":3000"))
}
