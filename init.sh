#!/bin/bash

# Abort on error
set -e

use_local_env=0
filename=""
allowed_environments=("local" "test" "prod")
environments=()
compose_files=("docker/compose-app-snippet.yml")

for arg in "$@"; do
    case $arg in
    --local)
        environments+=("local")
        shift
        ;;
    --test)
        environments+=("test")
        shift
        ;;
    --prod)
        environments+=("prod")
        shift
        ;;
    esac
done

while getopts "e:" arg_value; do
    case "${arg_value}" in
    e)
        filename=$OPTARG
        use_local_env=1
        ;;
    *)
        echo "Usage: init.sh --{local|test|prod} [-e {env_var_file}]"
        exit 1
        ;;
    esac
done

for env_name in ${environments[@]}; do

    echo "****** Setting up the $env_name environment..... ******"
    if [[ "$use_local_env" == 0 ]]; then
        filename=.env_$env_name
    fi

    allowed_hosts=""
    if [[ "$env_name" == "local" ]]; then
        allowed_hosts="localhost"
    elif [[ "$env_name" == "test" ]]; then
        allowed_hosts="gift-registry-test.hydrick-dev.net"
    elif [[ "$env_name" == "prod" ]]; then
        allowed_hosts="gift-registry.hydrick-dev.net"
    else
        echo "Environment $env_name is not allowed."
        exit -1
    fi

    # Include a docker compose file that undos the "run local" setup
    if [[ "$env_name" != "local" ]]; then
        compose_files+=("docker/compose-app-remote-snippet.yml")
    fi

    # Write the environment variable file
    if [[ "$use_local_env" == 0 ]]; then
        sample_file="docker/env_sample_docker_secrets"
        cp $sample_file $filename
    fi

    # Figure out if we need to include telemetry containers
    read -p "Do you want to export telemetry data to an external service? (Y/N) [Y] " exportTelem
    if [[ "$exportTelem" == [yY] ]]; then
        read -p "Do you have an existing service for telemetry data (if not we'll set up a local version)? (Y/N) [Y] " existingTelem
        if [[ ! "$existingTelem" == [yY] ]]; then
            collector="https://gift-registry-collector:4318"
            read -p "Do you want to bundle an observability back-end with the gift registry application? (Y/N) [Y] " createLGTM
            if [[ "$createLGTM" == [yY] ]]; then
                compose_files+=("docker/compose-observability-snippet.yml")
            fi
        fi
        compose_files+=("docker/compose-collector-snippet.yml")
        echo "OTEL_EXPORTER_OTLP_ENDPOINT=$collector" >>$filename
    fi

    output_file="docker-compose-$env_name.yml"
    merged_files=""
    for comp_file in ${compose_files[@]}; do
        merged_files="$merged_files -f $comp_file"
    done

    if [[ "$use_local_env" == 0 ]]; then
        sed --in-place -e "s/env_name/$env_name/g" $filename
    fi
    merge_command="docker compose --env-file=$filename $merged_files config > $output_file"
    eval $merge_command

    echo "Compose file $output_file has been built. The associated environment variables are in $filename. You need to make sure the sensitive values are stored in the associated Docker secrets."
    unset env_name

done
