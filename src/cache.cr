require "kemal"
require "json"
require "redis"
require "log"

Log.setup_from_env(default_level: :info)

Log.info { "Starting the application" }

def config
  # Read config file
  config_file = File.read("config/config.json")

  # Parse the bytes into the interface (unstructured data)
  JSON.parse(config_file).as_h
end

# Load the config file
config = config()

# Create a new redis client
redis = Redis::PooledClient.new(host: config["redis_host"].as_s, port: config["redis_port"].as_i, pool_size: 10, pool_timeout: 5.0)

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
  key : String = env.params.query["key"]? || ""
  if key.nil? || key.empty?
    halt env, status_code: 400, response: "key query parameter is required"
  end

  # Check the cache for the key
  val : String | Nil = redis.get(key)

  # If the item is not found, return a 404 error
  if val.nil? || val.empty?
    halt env, status_code: 404, response: "key not found"
  end

  # convert the value to a string
  val = val.to_s

  # Get the item's TTL in Redis
  item_ttl : Int64 = redis.ttl(key)

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
  begin
    # Parse the request body
    key : String = env.params.json["key"].as(String)
    value : String = env.params.json["value"].as(String)

    # check to see if the ttl is provided in the request body, if not the default ttl will be used later on
    begin
      # first try to cast the ttl to a string
      ttl = env.params.json["ttl"]?.try(&.as(String))
      ttl = nil if !ttl.nil? && ttl.empty?
    rescue e : TypeCastError
      # if that fails, assume the ttl is a whole number
      ttl = env.params.json["ttl"]
    end

    if key.empty? || value.empty?
      halt env, status_code: 400, response: "key and value params are required in payload body"
    end

    # if the ttl is nil, use the default ttl
    ttl = config["ttl"].as_i if ttl.nil?

    # if the ttl happens to be a string, convert it to an integer now
    ttl = ttl.to_i if ttl.is_a?(String)

    # Add the item to the cache
    redis.set(key, value, ex: ttl)
  rescue e : JSON::ParseException
    Log.error { "JSON parsing error: #{e.message}" }
    halt env, status_code: 400, response: {"error" => "Malformed JSON input"}.to_json
  rescue e : Redis::Error
    # if the error is due to the payload being too large, return a 400 error
    if e.message.try(&.includes?("ERR Protocol error: too big bulk count string"))
      Log.warn { "payload over 512mb, cannot cache due to hard limits in redis" }
      halt env, status_code: 400, response: "payload too large to cache"
    end

    Log.debug { "failed item: #{key} (type: #{key.class}) - #{value} (type: #{value.class}) - #{ttl} (type: #{ttl.class})" }

    Log.error { "Failed to cache item in redis: #{e.message} - #{e.backtrace}" }
    halt env, status_code: 500, response: "failed to cache item"
  end

  {message: "cached"}.to_json
end

# Start the application on 0.0.0.0:8080
Kemal.run(8080)
