.PHONY: all test proxy-penguin frontend

all:

proxy-penguin:
	go build ./cmd/proxy-penguin

frontend:
	cd frontend && npx vite build

prod: frontend
	go build -ldflags="-s -w" -trimpath ./cmd/proxy-penguin
