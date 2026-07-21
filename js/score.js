export function calculateScore(station){
    let score = 100;

    // ph value
    if (station.measurements.ph.value < 6.5 || station.measurements.ph.value > 8.5){
        score -= 20;
    }

    //dissolved oxygen
    if (station.measurements.dissolved_oxygen.value < 8){
        score -= 25;
    }

    // turbidity(suspension of particles in water causing visibility to change)
    if (station.measurements.turbidity.value > 5){

        score -=20;

    }

    // nitrates(pollutant)
    if (station.measurements.nitrate.value > 2){
        score -=15;
    }

    return score;
}

export function getColor(score){


    if (score>=90){return "green"};


    if (score>=70){return "yellow"};


    return "red";


}
