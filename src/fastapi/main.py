from fastapi import FastAPI

app = FastAPI()


@app.get("/api/cache")
def read_root():
    return {"Hello": "World"}


@app.get("/health")
def read_root():
    return "OK"
