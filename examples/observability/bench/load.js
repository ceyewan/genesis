import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Counter } from 'k6/metrics';

const errorCount = new Counter('orders_error_total');

function intEnv(name, defaultValue, minValue = 1) {
  const value = Number.parseInt(__ENV[name] || '', 10);
  if (!Number.isFinite(value) || value < minValue) {
    return defaultValue;
  }
  return value;
}

function strEnv(name, defaultValue) {
  return __ENV[name] || defaultValue;
}

function intListEnv(name, defaultCsv) {
  const raw = strEnv(name, defaultCsv);
  const values = raw
    .split(',')
    .map((item) => Number.parseInt(item.trim(), 10))
    .filter((item) => Number.isFinite(item) && item > 0);

  if (values.length === 0) {
    return defaultCsv
      .split(',')
      .map((item) => Number.parseInt(item.trim(), 10))
      .filter((item) => Number.isFinite(item) && item > 0);
  }

  return values;
}

const profile = strEnv('LOAD_PROFILE', 'fixed'); // fixed | wave | weekday
const baseURL = strEnv('LOAD_URL', 'http://localhost:8080/orders');
const authHeader = strEnv('LOAD_AUTH', 'Bearer demo-token');
const products = strEnv('LOAD_PRODUCTS', 'A001,B001,C001,D001')
  .split(',')
  .map((item) => item.trim())
  .filter((item) => item.length > 0);

const fixedRate = intEnv('LOAD_RATE', 5);
const fixedDuration = strEnv('LOAD_DURATION', '10m');

const waveLowRate = intEnv('LOAD_WAVE_LOW_RATE', 80);
const waveHighRate = intEnv('LOAD_WAVE_HIGH_RATE', 300);
const waveCycles = intEnv('LOAD_WAVE_CYCLES', 6);
const waveRamp = strEnv('LOAD_WAVE_RAMP', '2m');
const waveHold = strEnv('LOAD_WAVE_HOLD', '8m');

const weekdayStep = strEnv('LOAD_WEEKDAY_STEP', '5m');
const weekdayCycles = intEnv('LOAD_WEEKDAY_CYCLES', 2);
const weekdayPattern = intListEnv(
  'LOAD_WEEKDAY_PATTERN',
  // 工作日：晨峰爬升 -> 午间回落 -> 下午次峰 -> 晚间回落
  '80,120,180,260,360,500,680,820,760,690,620,560,620,720,840,980,1120,1020,900,780,660,520,400,300,220,150'
);

const preAllocatedVUs = intEnv('LOAD_PRE_ALLOCATED_VUS', 200);
const maxVUs = intEnv('LOAD_MAX_VUS', 2000);

function waveStages() {
  const stages = [];
  for (let i = 0; i < waveCycles; i += 1) {
    stages.push({ target: waveHighRate, duration: waveRamp });
    stages.push({ target: waveHighRate, duration: waveHold });
    stages.push({ target: waveLowRate, duration: waveRamp });
    stages.push({ target: waveLowRate, duration: waveHold });
  }
  return stages;
}

function weekdayStages() {
  const stages = [];
  for (let cycle = 0; cycle < weekdayCycles; cycle += 1) {
    for (let i = 0; i < weekdayPattern.length; i += 1) {
      stages.push({ target: weekdayPattern[i], duration: weekdayStep });
    }
  }
  return stages;
}

function buildScenario() {
  if (profile === 'wave') {
    return {
      executor: 'ramping-arrival-rate',
      startRate: waveLowRate,
      timeUnit: '1s',
      preAllocatedVUs,
      maxVUs,
      stages: waveStages(),
      gracefulStop: '30s',
    };
  }

  if (profile === 'weekday') {
    return {
      executor: 'ramping-arrival-rate',
      startRate: weekdayPattern[0],
      timeUnit: '1s',
      preAllocatedVUs,
      maxVUs,
      stages: weekdayStages(),
      gracefulStop: '30s',
    };
  }

  return {
    executor: 'constant-arrival-rate',
    rate: fixedRate,
    timeUnit: '1s',
    duration: fixedDuration,
    preAllocatedVUs,
    maxVUs,
    gracefulStop: '30s',
  };
}

export const options = {
  scenarios: {
    orders: buildScenario(),
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    checks: ['rate>0.95'],
    http_req_duration: ['p(95)<1500', 'p(99)<2500'],
  },
};

export default function () {
  const iteration = exec.scenario.iterationInTest;
  const vu = exec.vu.idInInstance;
  const product = products[iteration % products.length];
  const payload = JSON.stringify({
    user_id: `k6_user_${vu}_${iteration % 10000}`,
    product_id: product,
  });

  const res = http.post(baseURL, payload, {
    headers: {
      'Content-Type': 'application/json',
      Authorization: authHeader,
    },
    tags: {
      endpoint: 'orders',
      scenario: profile,
    },
  });

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
  });

  if (!ok) {
    errorCount.add(1);
  }
}
