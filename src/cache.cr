require "kemal"
require "json"
require "redis"

def config
  # Read config file
  config_file = File.read("config/config.json")

  # Parse the bytes into the interface (unstructured data)
  JSON.parse(config_file).as_h
end

# Load the config file
config = config()

# Create a new redis client
redis = Redis.new(host: config["redis_host"].as_s, port: config["redis_port"].as_i)

# Health endpoint
get "/health" do
  "OK"
end

# API Health endpoint
get "/api/health" do
  "OK"
end

# Endpoint to fetch an item from the in-memory redis cache
# If the item is found, the value of the item is returned
# If the item is not found, a 404 error is returned
get "/api/cache" do |env|
  # Get and validate the key query string parameter
  key = env.params.query["key"]? || ""
  if key.empty?
    halt env, status_code: 400, response: "key query parameter is required"
  end

  # Check the cache for the key
  val = redis.get(key)

  # If the item is not found, return a 404 error
  if val.nil?
    halt env, status_code: 404, response: "key not found"
  end

  # Get the items TTL in Redis
  item_ttl = redis.ttl(key)

  # Set the X-CACHE-TTL header for when the item expires
  env.response.headers["X-CACHE-TTL"] = item_ttl.to_s

  # Set a cache-control header to ensure the item is cached
  env.response.headers["Cache-Control"] = "public, max-age=#{item_ttl}"

  # Return the value of the item from the cache in JSON format
  val.to_json
end

# Endpoint to add an item to the in-memory redis cache
# If the item is successfully added, return a success message
post "/api/cache" do |env|
  # Parse the request body
  key = env.params.json["key"].as(String)
  value = env.params.json["value"].as(String)

  # check to see if the ttl is provided in the request body, if not the default ttl will be used later on
  ttl = env.params.json["ttl"]?.try(&.as(String))

  if key.empty? || value.empty?
    halt env, status_code: 400, response: "key and value params are required in payload body"
  end

  # Create the ttl variable to store the TTL of the item
  ttl = if ttl.nil?
          # If the TTL was not provided, use the default TTL from the config file
          # Fetch TTL from config file and convert it into a time.Duration in seconds
          config["ttl"].as_i.seconds
        else
          # If the TTL was provided, use it
          # Convert the string representation of the TTL into an integer
          ttl.to_i.seconds
        end

  # Add the item to the cache
  redis.set(key, value, ex: ttl.to_i)

  {message: "cached"}.to_json
end

# Start the application on 0.0.0.0:8080
Kemal.run(8080)
