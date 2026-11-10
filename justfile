# Run every check that does not need the game: `just check`.

check: lua companion public

lua:
	find addon -name '*.lua' -print0 | xargs -0 luac5.1 -p
	lua5.1 addon/tests/run.lua

companion:
	go vet ./...
	go test ./...
	GOOS=darwin go build ./...

# The binary lands where the dev service expects it: companion/ompcraft.
build:
	go build -o companion/ompcraft ./companion/cmd/ompcraft

# The repo is public: fail on real-looking home paths or private IPv4 addresses in tracked text.
# /home/user and /Users/demo are the placeholders tests use.
public:
	! grep -rnIP '(/home/(?!user\b)[a-z]|/Users/(?!demo\b)[A-Za-z]|\b(10|192\.168|172\.(1[6-9]|2[0-9]|3[01]))\.[0-9]+\.[0-9]+\.[0-9]+\b)' \
		--exclude-dir=.git --exclude=justfile --exclude-dir=tmp --exclude=ompcraft .
