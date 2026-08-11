module go-spring.org/starter-go-redis/example

go 1.26.1

require github.com/redis/go-redis/v9 v9.21.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

require go-spring.org/cloud v0.0.0

replace go-spring.org/cloud => ../../../cloud
