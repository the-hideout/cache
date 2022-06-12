from fastapi import FastAPI, HTTPException, Response
from pydantic import BaseModel
import redis
import json

# Read the json config file and load it into a dictionary
with open('config.json') as config_file:
    config = json.load(config_file)

app = FastAPI()
red = redis.Redis(host=config['redis_host'], port=config['redis_port'])
TTL = config['ttl']

# A schema for storing items in the in-memory cache
# key: The base64 encoded graphql query
# value: The graphql response for the given query
class Item(BaseModel):
    key: str
    value: str

# Endpoint to fetch an item from the in-memory redis cache
# If the item is found, the value of the item is returned
# If the item is not found, a 404 error is returned
@app.get("/api/cache")
async def cache(response: Response, key: str = False):
    if not key:
        raise HTTPException(status_code=400, detail="key query parameter is required")

    # Attempt to fetch the item from the cache
    cache = red.get(key)

    # If the item is not found, return a 404 error
    if not cache:
        raise HTTPException(status_code=404, detail="key not found")

    # Set the X-CACHE-TTL header for when the item expires
    response.headers["X-CACHE-TTL"] = str(red.ttl(key))

    # Return the value of the item
    return cache

# Endpoint to add an item to the in-memory redis cache
# If the item is successfully added, return a success message
@app.post("/api/cache")
async def cache(item: Item):
    if not item:
        raise HTTPException(status_code=400, detail="payload is required")

    # Add the item to the cache
    red.set(item.key, item.value)

    # Set the expiration of the item in the cache
    red.expire(item.key, TTL)

    return {"message": "cached"}

@app.get("/health")
async def health():
    return "OK"
