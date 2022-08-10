package main

import (
	"fmt"
	"net/http"

	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/gin-gonic/gin"
)

func config() map[string]interface{} {
	// Read config file
	configFile, err := os.Open("config.json")
	if err != nil {
		os.Exit(1)
	}
	defer configFile.Close()

	// Read the config file into bytes
	byteValue, _ := ioutil.ReadAll(configFile)

	// Define the interface to unmarshal
	var result map[string]interface{}

	// Parse the bytes into the interface (unstructured data)
	json.Unmarshal([]byte(byteValue), &result)

	// Return the interface (dict) of values
	return result
}

func main() {

	// Load the config file
	config := config()
	fmt.Printf("%+v\n", config)

	// Create a new gin router
	r := gin.Default()

	// Health endpoint
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// Start the application on 0.0.0.0:8080
	r.Run()
}
