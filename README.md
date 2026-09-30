# water-api

## Execution Instructions
These are the instructions for running the go server for the site. They assume you are running linux with a POSIX compliant shell, but
the instructions apply to most systems.

First, ensure that you have the newest changes, with `git pull`, and have `go` installed.

### Environment Variables
Make sure the following variables are set:
- PORT: the port on which the server will listen for http requests
- API_KEY: the USGS API key to use for requests to their API
- DATABASE_PORT: the port on localhost you are running postgresql
- DATABASE_PASSWORD: your user password, assumes login username 'postgres'

If you don't know how to set environment variables, use `export VARIABLE=VALUE`, with no spaces between the equals sign and the values
to each side

### Execution
Run the command `go run ./server/main` in the project root to start the server. Make sure the site works by visiting `localhost:$PORT` in
your preferred web browser, ensuring that you replace `$PORT` with the port you set earlier in the environment variables.
