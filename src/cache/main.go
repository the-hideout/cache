package main

import (
	"fmt"
	"net/http"

	"context"
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v9"
)

var ctx = context.Background()

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

	// Create a new redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%.0f", config["redis_host"], config["redis_port"]),
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// test 1
	err := rdb.Set(ctx, "key", "value", 0).Err()
	if err != nil {
		panic(err)
	}

	// test 2
	val, err := rdb.Get(ctx, "key").Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("key", val)

	// Create a new gin router
	r := gin.Default()

	// Health endpoint
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// Start the application on 0.0.0.0:8080
	r.Run()
}
