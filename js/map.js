import { calculateScore, getColor } from "./score.js";

export async function createMap() {
  const map = L.map('map').setView([43, -75], 7); // Albany, NY, leaflet map

  L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", { attribution: '© OpenStreetMap' }).addTo(map);

  let response = await fetch("../data/sample.json");

  let stations = await response.json().catch(async () => await (await fetch('../data/stations.json')).json());

  stations.forEach(station => {
    let score = calculateScore(station);
    let marker = L.circleMarker(
      [station.location.lat, station.location.lng], { radius: 12, color: getColor(score), fillColor: getColor(score) }
    );

    marker.addTo(map);

    marker.on("click", () => {
      const stationDiv = document.getElementById("station");

      stationDiv.innerHTML = "";

      const title = document.createElement("h2");
      title.textContent = station.name;

      const scoreTitle = document.createElement("h3");
      scoreTitle.textContent = `AquaScore: ${score * 100}/100`;

      const ph = document.createElement("p");
      ph.textContent = `pH: ${station.measurements.ph.value}`;

      const oxygen = document.createElement("p");
      oxygen.textContent = `Dissolved Oxygen: ${station.measurements.dissolved_oxygen.value}`;

      const turbidity = document.createElement("p");
      turbidity.textContent = `Turbidity: ${station.measurements.turbidity.value}`;

      const nitrate = document.createElement("p");
      nitrate.textContent = `Nitrate: ${station.measurements.nitrate.value}`;

      stationDiv.append(
        title,
        scoreTitle,
        ph,
        oxygen,
        turbidity,
        nitrate
      );
    });
  }
  )
}
