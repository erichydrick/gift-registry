all: observability 

observability:
	echo "Building Jaeger image..."
	cd docker/jaeger && docker build -t gift-registry-jaeger .
	echo "Building Prometheus image..."
	cd docker/prometheus && docker build -t gift-registry-prometheus .
