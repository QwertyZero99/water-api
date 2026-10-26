# water-api

As of now, there is no reliable, user-friendly way to obtain water quality info in the state of New York. Certain apps are regionalized to one specific region of New York, and governmental websites primarily focus on displaying raw data, not on being accessible to every NY resident, or anyone curious about New York’s water. For the benefit of every New Yorker curious about the quality of their drinking water and natural waterbodies, we created AquaNY. 

## Implementation

PostgreSQL allows us to feed the data to the Go backend, which allows data to be brought to the front end. Javascript was utilized to allow the backend to implement the frontend. We used HTML that serves as the backbone for the text on our website, and implemented CSS to make the text more pleasing to the eye

## Execution Instructions
These are the instructions for running the go server for the site. They assume you are running linux with a POSIX compliant shell, but
the instructions apply to most systems.

First, ensure that you have the newest changes, with `git pull`, and have `go` installed.

### Environment Variables
Make sure the following variables are set:
- PORT: the port on which the server will listen for http requests
- API_KEY: the USGS API key to use for requests to their API
- DATABASE_PORT: the port on localhost you are running postgresql
- DATABASE_PASSWORD: your postgresql password
- DATABASE_USERNAME: your postgresql user name
- UPDATE_STATIONS: if "YES", will update hourly

If you don't know how to set environment variables, use `export VARIABLE=VALUE`, with no spaces between the equals sign and the values
to each side

For Windows: `$env:VARIABLE = "VALUE"`

### Execution
Run the command `go run ./server/main` in the project root to start the server. Make sure the site works by visiting `localhost:$PORT` in
your preferred web browser, ensuring that you replace `$PORT` with the port you set earlier in the environment variables.
