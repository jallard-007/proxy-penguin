.PHONY: all test proxy-penguin frontend

proxy-penguin:
	go build ./cmd/proxy-penguin

frontend:
	cd frontend && npx vite build

all: frontend proxy-penguin
