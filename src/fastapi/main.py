from fastapi import FastAPI, HTTPException

app = FastAPI()


@app.get("/api/cache")
async def cache(payload: str = False):
    if not payload:
        raise HTTPException(status_code=400, detail="payload query parameter is required")

    # NOTE: Here we can interact with the payload which will be a graphql query
    # We can do sorting, inspection, etc
    # For now, we just want cloudflare to cache the response so we return a 200

    return "OK"


@app.get("/health")
async def health():
    return "OK"
