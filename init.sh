#!/bin/bash
set -e 
# Install needed utilities
go install honnef.co/go/tools/cmd/staticcheck@latest

environments=("local" "test" "prod")
compose_files=("docker/docker-compose-app.yml")

for env_name in ${environments[@]}; do

    echo "****** Setting up the $env_name environment..... ******"
    allowed_hosts=""
    if [[ "$env_name" == "local" ]]; then
        allowed_hosts="localhost"
    elif [[ "$env_name" == "test" ]]; then
        allowed_hosts="gift-registry-test.hydrick-dev.net"
    elif [[ "$env_name" == "prod" ]]; then
        allowed_hosts="gift-registry.hydrick-dev.net"
    fi

    # Write the environment variable file
    filename="./docker/.env_$env_name"

    echo "# Server values" > $filename
    echo "ALLOWED_HOSTS=$allowed_hosts" >> $filename
    echo "PORT=8080" >> $filename
    echo "" >> $filename

    # Login emailer credentails
    echo "# Email configurations" >> $filename
    echo "# TODO: CHANGE THESE" >> $filename
    echo "EMAIL_FROM=me@me.com" >> $filename
    echo "EMAIL_HOST=localhost" >> $filename
    echo "EMAIL_PASS=password" >> $filename
    echo "EMAIL_PORT=8000" >> $filename
    echo "" >> $filename
    
    # Locations for files the app will need 
    echo "# Directory locations used by the app" >> $filename
    echo "MIGRATIONS_DIR=migrations" >> $filename
    echo "STATIC_FILES_DIR=." >> $filename
    echo "TEMPLATES_DIR=templates" >> $filename
    echo "" >> $filename

    # Database configurations
    # TODO: REMOVE THESE WHEN I SWITCH TO SQLITE
    echo "DB_NAME=gift_registry" >> $filename
    echo "DB_PASS=gift_registry_password" >> $filename
    echo "DB_USER=gift_registry_user" >> $filename
    echo "" >> $filename
    
    # Observability configuration
    echo "# Observability configuration" >> $filename
    echo "OTEL_METRIC_EXPORT_INTERVAL=5000" >> $filename

    # Figure out if we need to include telemetry data
    read -p "Do you want to export observability data to an external service? (Y/N) [Y] " exportTelem
    if [[ "$exportTelem" == [yY] ]]; then
        compose_files+=("docker/docker-compose-collector.yml")
        echo "OTEL_EXPORTER_OTLP_ENDPOINT=http://gift-registry-collector:4318" >> $filename
        read -p "Do you want to build an observability back-end for sending observability data to? (Y/N) [Y] " createLGTM
        if [[ "$createLGTM" == [yY] ]]; then
            compose_files+=("docker/docker-compose-observability.yml")
        fi
    fi

    output_file="docker/docker-compose-$env_name.yml" 
    merged_files=""
    for comp_file in ${compose_files[@]}; do 
        merged_files="$merged_files -f $comp_file"
    done
    merge_command="docker compose ${merged_files} config > ${output_file}"
    eval $merge_command

done
