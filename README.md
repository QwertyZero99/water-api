# water-api
This is the web server for AquaNY, a website designed to allow those who live in NY to access real time and historical water quality data to improve their quality of life. The web server provides many functionalities, including server the web page frontend, managing the database of water data, and providing intrasite APIs for search and map systems.

The main server is written in Go, using the standard "net/http" package. A seperate machine learning model, used to predict future water trends, is written using Python. The database used is PostgreSQL, allowing for simple but secure storage of data.

## Execution Instructions
These are the instructions for running the go server for the site. They assume you are running Linux/UNIX with a POSIX compliant shell, but
the instructions apply to most systems.

First, ensure that you have the newest changes, with `git pull`, and have `go` installed.

### Environment Variables
Make sure the following variables are set:
- PORT: the port on which the server will listen for http requests
- API_KEY: the USGS API key
- DATABASE_PORT: the port on localhost you are running postgresql
- DATABASE_USERNAME: your user name
- DATABASE_PASSWORD: your user password
- UPDATE_STATIONS: whether to update USGS station data periodically

### Execution
Run the command `go run ./server/main` in the project root to start the server. Make sure the site works by visiting `localhost:$PORT` in
your preferred web browser, ensuring that you replace `$PORT` with the port you set earlier in the environment variables.
