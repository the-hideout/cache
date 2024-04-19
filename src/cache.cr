require "kemal"

# Define your routes
get "/" do
  "Hello World!"
end

# Run Kemal on port 8080
Kemal.run(8080)
