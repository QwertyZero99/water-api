import { calculateScore, getColor } from "./score.js";

export async function createMap(){
    const map = L.map('map').setView([43, -75], 7); // Albany, NY, leaflet map

    let counter = 0;

    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png",{attribution:'© OpenStreetMap'}).addTo(map);



    let response = await fetch("../data/sample.json");


    let stations = await response.json().catch(async()=>await (await fetch('../data/stations.json')).json());



    stations.forEach(station=>{
        let score = calculateScore(station);
        let marker = L.circleMarker(
            [station.location.lat,station.location.lng], {radius:12, color:getColor(score)}
        );



        marker.addTo(map);

        marker.on("click", 
                    ()=>{
            document.getElementById("station").innerHTML=
            `

            <h2>${station.name}</h2>

            <h3>
            AquaScore:
            ${score}/100
            </h3>

            <p>
            pH: ${station.measurements.ph.value}
            </p>

            <p>
            Dissolved Oxygen:
            ${station.measurements.dissolved_oxygen.value}
            </p>

            <p>
            Turbidity:
            ${station.measurements.turbidity.value}
            </p>

            <p>
            Nitrate:
            ${station.measurements.nitrate.value}
            </p>

            `

            }

        );


    }
    )


}
