.PHONY: all test proxy-penguin

all: proxy-penguin

proxy-penguin:
	go build ./cmd/proxy-penguin
