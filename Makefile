.PHONY: all test proxy-penguin frontend

all: proxy-penguin

frontend:
	cd frontend && npm install && npx vite build

proxy-penguin: frontend
	go build ./cmd/proxy-penguin
