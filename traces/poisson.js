// k6 run poisson.js --out csv=latencies.csv --no-thresholds --no-summary

import http from 'k6/http';
import { sleep } from 'k6';
import { SharedArray } from 'k6/data';
import exec from 'k6/execution';

const targetURL = __ENV.TARGET_URL;
const scheduleFile = __ENV.SCHEDULE_FILE || './flattened_schedule.json';
const virtualUsers = Number.parseInt(__ENV.VUS || '3000', 10);
const maxDuration = __ENV.MAX_DURATION || '4000s';
const requestTimeout = __ENV.REQUEST_TIMEOUT || '1s';

if (!targetURL) {
  throw new Error('TARGET_URL must be set');
}
if (!Number.isInteger(virtualUsers) || virtualUsers < 1) {
  throw new Error('VUS must be a positive integer');
}

const delays = new SharedArray('delays', () =>
  JSON.parse(open(scheduleFile))
);

export const options = {
  discardResponseBodies: true,
  scenarios: {
    replay: {
      executor: 'shared-iterations',
      vus: virtualUsers,
      iterations: delays.length,
      maxDuration,
    },
  },
};

export default function () {
  const index = exec.scenario.iterationInTest;
  const delay = delays[index];

  const testStart = exec.scenario.startTime / 1000;
  const now = Date.now() / 1000;
  const timeSinceStart = now - testStart;
  const wait = delay - timeSinceStart;

  if (wait > 0) {
    sleep(wait);
  }

  http.get(targetURL, { timeout: requestTimeout });
}
