//TODO use temperature to weight some metrics that depend on it, like dissolved_oxygen

const metrics = {
  ph: {
    weight: 0.25,
    targetMin: 6.5,
    targetMax: 8.5,
    min: 4,
    max: 10,
  },
  dissolved_oxygen: {
    weight: 0.3,
    targetMin: 8,
    targetMax: 14,
    min: 0,
    max: 20,
  },
  turbidity: {
    weight: 0.2,
    targetMin: 0,
    targetMax: 5,
    min: 0,
    max: 50,
  },
  nitrate: {
    weight: 0.25,
    targetMin: 0,
    targetMax: 2,
    min: 0,
    max: 20,
  },
};

/**
* @returns a float between 0 and 1, representing the score of the station
*/
export function calculateScore(station) {
  const measurements = station.measurements;

  const scores = {
    ph: scoreRange(measurements.ph.value, metrics.ph),
    dissolved_oxygen: scoreRange(
      measurements.dissolved_oxygen.value,
      metrics.dissolved_oxygen
    ),
    turbidity: scoreRange(
      measurements.turbidity.value,
      metrics.turbidity
    ),
    nitrate: scoreRange(
      measurements.nitrate.value,
      metrics.nitrate
    ),
  };

  const finalScore = Object.entries(scores).reduce(
    (total, [key, value]) =>
      total + value * metrics[key].weight,
    0
  );

  return finalScore;
}

export function getColor(score) {
  if (score >= 0.9) return "green";
  if (score >= 0.7) return "yellow";
  return "red";
}

function scoreRange(value, metric) {
  const { targetMin, targetMax, min, max } = metric;

  // perfect score
  if (value >= targetMin && value <= targetMax) {
    return 1;
  }

  // dist from target
  const distance =
    value < targetMin
      ? targetMin - value
      : value - targetMax;

  const maxDistance =
    value < targetMin
      ? targetMin - min
      : max - targetMax;

  const score = 1 - distance / maxDistance;

  return Math.max(0, Math.min(1, score));
}

