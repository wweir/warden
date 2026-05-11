function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function bindPollState(waitingAlive, waitingElapsed) {
  return {
    onStart: () => {
      waitingAlive.value = true;
      waitingElapsed.value = 0;
    },
    onTick: (elapsedSeconds) => {
      waitingElapsed.value = elapsedSeconds;
    },
    onStop: () => {
      waitingAlive.value = false;
      waitingElapsed.value = 0;
    },
  };
}

export async function pollUntilAlive(
  fetchStatus,
  {
    timeoutMs = 60000,
    intervalMs = 1500,
    initialDelayMs = 800,
    onStart = () => {},
    onTick = () => {},
    onStop = () => {},
  } = {},
) {
  const deadline = Date.now() + timeoutMs;
  const startMs = Date.now();
  onStart();
  const ticker = setInterval(() => {
    onTick(Math.floor((Date.now() - startMs) / 1000));
  }, 500);
  try {
    await sleep(initialDelayMs);
    while (Date.now() < deadline) {
      try {
        await fetchStatus();
        return true;
      } catch {
        await sleep(intervalMs);
      }
    }
    return false;
  } finally {
    clearInterval(ticker);
    onStop();
  }
}
