package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v9"

	sentry "github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
)

// A schema for storing items in the in-memory cache
// key: The base64 encoded graphql query
// value: The graphql response for the given query
type CacheSetBody struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Ttl   string `json:"ttl"`
}

var ctx = context.Background()

func config() map[string]interface{} {
	// Read config file
	configFile, err := os.Open("config.json")
	if err != nil {
		panic(err)
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

	err := sentry.Init(sentry.ClientOptions{
		Dsn: "https://4b25273b58ccdf45a8d574fc0a26dbee@sentry.thaddeus.io/5",
		// Set TracesSampleRate to 1.0 to capture 100%
		// of transactions for performance monitoring.
		// We recommend adjusting this value in production,
		EnableTracing:    true,
		TracesSampleRate: 1,
		Debug:            true,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	// Load the config file
	config := config()

	// Create a new redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%.0f", config["redis_host"], config["redis_port"]),
		Password: "", // no password set
		DB:       0,  // use default DB
		PoolSize: 20, // use a pool size of 20
	})

	// Create a new gin router
	r := gin.Default()

	// Use the sentry middleware
	r.Use(sentrygin.New(sentrygin.Options{}))

	// Health endpoint
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// API Health endpoint
	r.GET("/api/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// Endpoint to fetch an item from the in-memory redis cache
	// If the item is found, the value of the item is returned
	// If the item is not found, a 404 error is returned
	r.GET("/api/cache", func(c *gin.Context) {
		// Get and validate the key query string parameter
		key := c.DefaultQuery("key", "")
		if key == "" {
			c.String(http.StatusBadRequest, "key query parameter is required")
			return
		}

		sentry.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("query.key", key)
		})

		// Check the cache for the key
		val, err := rdb.Get(ctx, key).Result()

		// If the item is not found, return a 404 error
		if err == redis.Nil {
			c.String(http.StatusNotFound, "key not found")
			return
			// If something else went wrong, return a 500 error
		} else if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Get the items TTL in Redis
		item_ttl, err := rdb.TTL(ctx, key).Result()
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		//Set the X-CACHE-TTL header for when the item expires
		c.Header("X-CACHE-TTL", fmt.Sprintf("%.0f", item_ttl.Seconds()))

		// Set a cache-control header to ensure the item is cached
		c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(item_ttl.Seconds())))

		// Return the value of the item from the cache
		c.JSON(http.StatusOK, val)
	})

	// Endpoint to add an item to the in-memory redis cache
	// If the item is successfully added, return a success message
	r.POST("/api/cache", func(c *gin.Context) {
		var requestBody CacheSetBody

		// Parse and validate the request body
		if err := c.BindJSON(&requestBody); err != nil {
			c.String(http.StatusBadRequest, "payload is required")
			return
		}
		if requestBody.Key == "" || requestBody.Value == "" {
			c.String(http.StatusBadRequest, "key and value params are required in payload body")
			return
		}

		sentry.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("query.key", requestBody.Key)
		})

		// Create the ttl variable to store the TTL of the item
		var ttl time.Duration

		// Check if the TTL was provided in the request body
		ttlString := requestBody.Ttl

		if ttlString == "" {
			// If the TTL was not provided, use the default TTL from the config file
			// Fetch TTL from config file and convert it into a time.Duration in seconds
			ttl = time.Duration(int(config["ttl"].(float64))) * time.Second
		} else {
			// If the TTL was provided, use it
			// Convert the string representation of the TTL into an integer
			ttlInt, err := strconv.Atoi(requestBody.Ttl)

			// Throw an error if we can't convert the TTL into an integer
			if err != nil {
				c.String(http.StatusBadRequest, "ttl must be an string representation of an integer")
				return
			}

			// Convert the TTL into a time.Duration in seconds
			ttl = time.Duration(int(ttlInt)) * time.Second
		}

		sentry.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTag("query.ttl", ttl.String())
		})

		// Add the item to the cache
		err := rdb.Set(ctx, requestBody.Key, requestBody.Value, ttl).Err()
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "cached"})
	})

	// Start the application on 0.0.0.0:8080
	r.Run(":8080")
}
