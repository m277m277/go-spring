module go-spring.org/observe-resilience

go 1.26

require (
	go-spring.org/log v0.1.4
	go-spring.org/observe v0.0.0
	go-spring.org/spring v1.3.4
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/metric v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
)

replace go-spring.org/observe => ../observe
