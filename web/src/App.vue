<template>
  <div class="min-h-screen bg-ink text-white">
    <div class="absolute inset-0 grid-glow scanline opacity-70"></div>
    <div class="relative mx-auto max-w-6xl px-6 py-10">
      <header class="flex flex-col gap-6 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p class="text-sm uppercase tracking-[0.3em] text-fog">serman</p>
          <h1 class="mt-2 text-3xl font-semibold sm:text-4xl">
            Monochrome core,
            <span class="text-neon">neon pulse</span>
          </h1>
          <p class="mt-3 max-w-xl text-sm text-fog">
            Monitor local services, validate Docker setup, and act fast without leaving the flow.
          </p>
        </div>
        <div class="flex items-center gap-3">
          <button
            class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm uppercase tracking-wider text-fog transition hover:border-neon/50 hover:text-white"
            @click="refreshAll"
          >
            Refresh
          </button>
          <span v-if="statusMessage" class="text-xs text-neon">{{ statusMessage }}</span>
        </div>
      </header>

      <section class="mt-10 grid gap-6 lg:grid-cols-[1.2fr_1fr]">
        <div class="glass neon-ring rounded-3xl p-6">
          <div class="flex items-center justify-between">
            <h2 class="text-lg font-semibold">Dependencies</h2>
            <span
              class="rounded-full border px-3 py-1 text-xs uppercase tracking-widest"
              :class="depsOk ? 'border-neon/60 text-neon' : 'border-red-500/50 text-red-300'"
            >
              {{ depsOk ? "ready" : "missing" }}
            </span>
          </div>
          <div class="mt-6 grid gap-4">
            <div class="flex items-center justify-between rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div>
                <p class="text-sm text-fog">Docker Engine</p>
                <p class="text-base">{{ deps.dockerOk ? deps.dockerVersion : "Not Found" }}</p>
              </div>
              <span
                class="h-2 w-2 rounded-full"
                :class="deps.dockerOk ? 'bg-neon shadow-neon' : 'bg-red-500'"
              ></span>
            </div>
            <div class="flex items-center justify-between rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
              <div>
                <p class="text-sm text-fog">Docker Compose</p>
                <p class="text-base">{{ deps.composeOk ? deps.composeVersion : "Not Found" }}</p>
              </div>
              <span
                class="h-2 w-2 rounded-full"
                :class="deps.composeOk ? 'bg-neon shadow-neon' : 'bg-red-500'"
              ></span>
            </div>
          </div>
          <p v-if="!depsOk" class="mt-4 text-xs text-red-300">
            Install Docker and Docker Compose to unlock service actions.
          </p>
        </div>

        <div class="glass rounded-3xl p-6">
          <h2 class="text-lg font-semibold">Quick Actions</h2>
          <p class="mt-2 text-sm text-fog">
            Select a service below to trigger start, stop, restart, or a clean fresh run.
          </p>
          <div class="mt-6 grid gap-3">
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm text-fog">
              <p class="text-xs uppercase tracking-widest text-fog">Auto refresh</p>
              <p class="mt-1 text-base text-white">Every 6 seconds</p>
            </div>
            <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm text-fog">
              <p class="text-xs uppercase tracking-widest text-fog">Network</p>
              <p class="mt-1 text-base text-white">Localhost API</p>
            </div>
          </div>
        </div>
      </section>

      <section class="mt-10 glass rounded-3xl p-6">
        <div class="flex flex-wrap items-center justify-between gap-4">
          <h2 class="text-lg font-semibold">Services</h2>
          <div class="flex flex-wrap items-center gap-3">
            <label class="relative block">
              <input
                v-model="searchQuery"
                type="text"
                class="w-64 rounded-full border border-white/10 bg-white/5 px-4 py-2 pr-10 text-sm text-white outline-none transition placeholder:text-white/25 focus:border-neon/50"
                placeholder="Search services..."
              />
              <span class="pointer-events-none absolute right-4 top-1/2 -translate-y-1/2 text-xs uppercase tracking-[0.25em] text-fog">
                Find
              </span>
            </label>
            <span class="text-xs uppercase tracking-widest text-fog">
              {{ filteredServices.length }} / {{ services.length }}
            </span>
          </div>
        </div>

        <div class="mt-6 overflow-hidden rounded-2xl border border-white/10">
          <table class="w-full border-collapse text-left text-sm">
            <thead class="bg-white/5 text-xs uppercase tracking-widest text-fog">
              <tr>
                <th class="px-4 py-3">Service</th>
                <th class="px-4 py-3">State</th>
                <th class="px-4 py-3">Health</th>
                <th class="px-4 py-3">Ports</th>
                <th class="px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="svc in filteredServices"
                :key="svc.name"
                class="border-t border-white/5 bg-white/0 transition hover:bg-white/5"
              >
                <td class="px-4 py-3 font-medium text-white">{{ svc.name }}</td>
                <td class="px-4 py-3">
                  <span
                    class="rounded-full border px-3 py-1 text-xs uppercase tracking-widest"
                    :class="stateClass(svc.state)"
                  >
                    {{ svc.state || "unknown" }}
                  </span>
                </td>
                <td class="px-4 py-3 text-fog">{{ svc.health || "—" }}</td>
                <td class="px-4 py-3 text-fog">{{ svc.ports || "—" }}</td>
                <td class="px-4 py-3">
                  <div class="flex flex-wrap justify-end gap-2">
                    <button
                      class="rounded-full border border-cyan-400/60 bg-cyan-500/10 px-3 py-1 text-xs uppercase tracking-widest text-cyan-100 transition hover:border-cyan-300 hover:bg-cyan-500/20 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-white/35"
                      :disabled="busy.has(svc.name)"
                      @click="openConfig(svc.name)"
                    >
                      Edit
                    </button>
                    <button
                      v-for="action in actions"
                      :key="action"
                      class="rounded-full border px-3 py-1 text-xs uppercase tracking-widest transition disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-white/35"
                      :class="actionClass(svc, action)"
                      :disabled="isActionDisabled(svc, action)"
                      @click="runAction(svc.name, action)"
                    >
                      {{ action }}
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="filteredServices.length === 0">
                <td class="px-4 py-6 text-center text-fog" colspan="5">No matching services found.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <div
      v-if="configModal.open"
      class="fixed inset-0 z-20 flex items-center justify-center bg-black/70 px-4 py-8 backdrop-blur-sm"
      @click.self="closeConfig"
    >
      <div class="glass w-full max-w-2xl rounded-3xl border border-white/10 p-6">
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-xs uppercase tracking-[0.3em] text-fog">service config</p>
            <h2 class="mt-2 text-2xl font-semibold text-white">{{ configModal.service }}</h2>
            <p class="mt-2 text-sm text-fog">Changes are saved to the project `.env` file.</p>
          </div>
          <button
            class="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-xs uppercase tracking-widest text-fog transition hover:border-white/30 hover:text-white"
            @click="closeConfig"
          >
            Close
          </button>
        </div>

        <div v-if="configModal.loading" class="mt-6 rounded-2xl border border-white/10 bg-white/5 px-4 py-6 text-sm text-fog">
          Loading config...
        </div>

        <div v-else-if="configModal.error" class="mt-6 rounded-2xl border border-rose-400/30 bg-rose-500/10 px-4 py-4 text-sm text-rose-200">
          {{ configModal.error }}
        </div>

        <form v-else class="mt-6 space-y-4" @submit.prevent="saveConfig">
          <label
            v-for="field in configModal.fields"
            :key="field.key"
            class="block rounded-2xl border border-white/10 bg-white/5 px-4 py-3"
          >
            <span class="text-xs uppercase tracking-[0.25em] text-fog">{{ field.label }}</span>
            <span class="mt-1 block text-xs text-white/35">{{ field.key }}</span>
            <input
              v-model="configModal.values[field.key]"
              type="text"
              class="mt-3 w-full border-0 bg-transparent p-0 text-sm text-white outline-none placeholder:text-white/25"
              :placeholder="field.label"
            />
          </label>

          <div class="flex flex-wrap justify-end gap-3 pt-2">
            <button
              type="button"
              class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm uppercase tracking-wider text-fog transition hover:border-white/30 hover:text-white"
              :disabled="configModal.saving"
              @click="closeConfig"
            >
              Cancel
            </button>
            <button
              type="submit"
              class="rounded-full border border-neon/60 bg-neon/10 px-4 py-2 text-sm uppercase tracking-wider text-neon transition hover:border-neon hover:bg-neon/20 disabled:cursor-not-allowed disabled:border-white/10 disabled:bg-white/5 disabled:text-white/35"
              :disabled="configModal.saving"
            >
              {{ configModal.saving ? "Saving..." : "Save config" }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";

const API = import.meta.env.VITE_API_BASE || "/api";

const deps = ref({
  dockerOk: false,
  dockerVersion: "",
  composeOk: false,
  composeVersion: ""
});
const services = ref([]);
const statusMessage = ref("");
const busy = ref(new Set());
const actions = ["start", "stop", "restart", "fresh"];
const searchQuery = ref("");
const configModal = ref({
  open: false,
  service: "",
  loading: false,
  saving: false,
  error: "",
  fields: [],
  values: {}
});

const depsOk = computed(() => deps.value.dockerOk && deps.value.composeOk);
const filteredServices = computed(() => {
  const query = searchQuery.value.trim().toLowerCase();
  if (!query) {
    return services.value;
  }
  return services.value.filter((svc) =>
    [svc.name, svc.state, svc.health, svc.ports]
      .filter(Boolean)
      .some((value) => value.toLowerCase().includes(query))
  );
});

let timer;

const loadDeps = async () => {
  const res = await fetch(`${API}/deps`);
  deps.value = await res.json();
};

const loadServices = async () => {
  const res = await fetch(`${API}/services`);
  if (!res.ok) {
    throw new Error("failed to fetch services");
  }
  services.value = await res.json();
};

const refreshAll = async () => {
  try {
    await loadDeps();
    await loadServices();
    statusMessage.value = "synced";
    setTimeout(() => (statusMessage.value = ""), 1500);
  } catch (err) {
    statusMessage.value = "sync error";
    setTimeout(() => (statusMessage.value = ""), 2000);
  }
};

const openConfig = async (service) => {
  configModal.value = {
    open: true,
    service,
    loading: true,
    saving: false,
    error: "",
    fields: [],
    values: {}
  };

  try {
    const res = await fetch(`${API}/config?service=${encodeURIComponent(service)}`);
    if (!res.ok) {
      throw new Error(await res.text());
    }
    const data = await res.json();
    const values = {};
    for (const field of data.fields || []) {
      values[field.key] = field.value || "";
    }
    configModal.value = {
      ...configModal.value,
      loading: false,
      fields: data.fields || [],
      values
    };
  } catch (err) {
    configModal.value = {
      ...configModal.value,
      loading: false,
      error: err.message || "Failed to load config"
    };
  }
};

const closeConfig = () => {
  if (configModal.value.saving) {
    return;
  }
  configModal.value = {
    open: false,
    service: "",
    loading: false,
    saving: false,
    error: "",
    fields: [],
    values: {}
  };
};

const saveConfig = async () => {
  configModal.value = { ...configModal.value, saving: true, error: "" };
  try {
    const res = await fetch(`${API}/config`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        service: configModal.value.service,
        values: configModal.value.values
      })
    });
    if (!res.ok) {
      throw new Error(await res.text());
    }
    statusMessage.value = "config saved";
    setTimeout(() => (statusMessage.value = ""), 1500);
    await loadServices();
    closeConfig();
  } catch (err) {
    configModal.value = {
      ...configModal.value,
      saving: false,
      error: err.message || "Failed to save config"
    };
  }
};

const runAction = async (service, action) => {
  if (busy.value.has(service)) {
    return;
  }
  busy.value.add(service);
  try {
    const res = await fetch(`${API}/action`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ service, action })
    });
    if (!res.ok) {
      throw new Error(await res.text());
    }
    await loadServices();
    statusMessage.value = `${action} ok`;
    setTimeout(() => (statusMessage.value = ""), 1500);
  } catch (err) {
    statusMessage.value = `${action} failed`;
    setTimeout(() => (statusMessage.value = ""), 2000);
  } finally {
    busy.value.delete(service);
  }
};

const normalizedState = (state = "") => state.toLowerCase().trim();

const isUnavailableState = (state = "") => normalizedState(state).includes("unavailable");

const isRunningState = (state = "") => {
  const s = normalizedState(state);
  return s.includes("running") || s.includes("healthy");
};

const isRestartingState = (state = "") => normalizedState(state).includes("restarting");

const isDownState = (state = "") => {
  const s = normalizedState(state);
  return s === "down" || s.includes("exited") || s.includes("created") || s.includes("stopped");
};

const isActionDisabled = (svc, action) => {
  if (!depsOk.value || busy.value.has(svc.name) || isUnavailableState(svc.state)) {
    return true;
  }

  switch (action) {
    case "start":
      return isRunningState(svc.state) || isRestartingState(svc.state);
    case "stop":
      return isDownState(svc.state);
    case "restart":
      return isDownState(svc.state);
    case "fresh":
      return false;
    default:
      return true;
  }
};

const actionClass = (svc, action) => {
  if (isActionDisabled(svc, action)) {
    return "";
  }

  switch (action) {
    case "start":
      return "border-emerald-400/60 bg-emerald-500/10 text-emerald-200 hover:border-emerald-300 hover:bg-emerald-500/20";
    case "stop":
      return "border-rose-400/60 bg-rose-500/10 text-rose-200 hover:border-rose-300 hover:bg-rose-500/20";
    case "restart":
      return "border-amber-400/60 bg-amber-500/10 text-amber-100 hover:border-amber-300 hover:bg-amber-500/20";
    case "fresh":
      return "border-sky-400/60 bg-sky-500/10 text-sky-100 hover:border-sky-300 hover:bg-sky-500/20";
    default:
      return "border-white/10 bg-white/5 text-fog hover:border-white/30 hover:text-white";
  }
};

const stateClass = (state = "") => {
  const s = state.toLowerCase();
  if (s.includes("running")) return "border-neon/60 text-neon";
  if (s.includes("down")) return "border-white/20 text-fog";
  if (s.includes("unavailable")) return "border-red-500/50 text-red-300";
  if (s.includes("paused") || s.includes("exited")) return "border-yellow-400/50 text-yellow-200";
  return "border-white/20 text-fog";
};

onMounted(async () => {
  await refreshAll();
  timer = setInterval(refreshAll, 6000);
});

onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
});
</script>
