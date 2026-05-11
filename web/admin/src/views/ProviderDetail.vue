<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">{{ $t("dashboard.title") }}</router-link>
      <span class="sep">/</span>
      <router-link to="/providers">{{ $t("providers.title") }}</router-link>
      <span class="sep">/</span>
      <span class="current">{{ pageTitle }}</span>
    </div>

    <h2 class="page-title">{{ pageTitle }}</h2>

    <div
      v-if="configSource && !configSource.source_type?.file"
      class="msg warning"
    >
      {{
        $t("config.nonFileWarning", {
          path: configSource.config_path || "remote",
        })
      }}
    </div>

    <div v-if="configFileChanged" class="msg warning">
      {{ $t("config.externalChange") }}
      <button @click="load" class="btn btn-sm">
        {{ $t("common.reload") }}
      </button>
    </div>

    <div v-if="message" class="msg success">{{ message }}</div>
    <div v-if="error" class="msg error">{{ error }}</div>

    <div v-if="loading" class="msg">{{ $t("common.loading") }}</div>
    <div v-else class="detail-layout">
      <section class="info-section">
        <div class="section-top">
          <div>
            <h3>{{ $t("providerDetail.configEditor") }}</h3>
            <p class="section-desc">
              {{ $t("providerDetail.configEditorDesc") }}
            </p>
          </div>
          <div class="actions">
            <button
              @click="apply"
              class="btn btn-primary"
              :disabled="
                saving || (configSource && !configSource.source_type?.file)
              "
            >
              {{
                saving
                  ? waitingAlive
                    ? $t("config.waitingService", { n: waitingElapsed })
                    : $t("providerDetail.saving")
                  : $t("providerDetail.saveApply")
              }}
            </button>
            <button
              v-if="dirty && !saving"
              @click="discard"
              class="btn btn-secondary"
            >
              {{ $t("config.discardChanges") }}
            </button>
            <button
              v-if="!create && !saving"
              @click="deleteProvider"
              class="btn btn-danger"
            >
              {{ $t("providerDetail.deleteProvider") }}
            </button>
          </div>
        </div>

        <div class="provider-form">
          <section class="form-panel primary-panel">
            <div class="form-panel-head">
              <div>
                <h4>{{ $t("providerDetail.quickSetupSection") }}</h4>
                <p class="section-desc">
                  {{ $t("providerDetail.quickSetupDesc") }}
                </p>
              </div>
              <div v-if="currentPreset" class="panel-badges">
                <span class="badge badge-ok">{{ currentPreset.title }}</span>
              </div>
            </div>
            <div class="form-grid">
              <label
                >{{ $t("providerDetail.providerType") }}
                <span class="req">*</span></label
              >
              <div class="field-stack">
                <select
                  :value="selectedAccessTypeId"
                  class="form-input"
                  @change="handleAccessTypeChange($event.target.value)"
                >
                  <option
                    v-for="option in accessTypeOptions"
                    :key="option.id"
                    :value="option.id"
                  >
                    {{ accessTypeTitle(option) }}
                  </option>
                </select>
                <p class="hint">{{ currentAccessTypeSummary }}</p>
              </div>

              <div v-if="isCustomAccessType" class="form-grid-full custom-interface-editor">
                <div class="custom-interface-head">
                  <span class="interface-preview-title">
                    {{ $t("providerDetail.customAccessSection") }}
                  </span>
                  <span class="hint">
                    {{ $t("providerDetail.customAccessDesc") }}
                  </span>
                </div>
                <div class="form-grid compact-grid">
                  <label
                    >{{ $t("providerDetail.family") }}
                    <span class="req">*</span></label
                  >
                  <select v-model="providerConfig.family" class="form-input">
                    <option value="">
                      {{ $t("providerDetail.selectFamily") }}
                    </option>
                    <option value="openai">openai</option>
                    <option value="anthropic">anthropic</option>
                    <option value="copilot">copilot</option>
                  </select>

                  <template v-if="providerFamily(providerConfig) === 'openai'">
                    <label>backend</label>
                    <select v-model="providerConfig.backend" class="form-input">
                      <option value="">default</option>
                      <option value="cliproxy">cliproxy</option>
                    </select>

                    <template v-if="providerBackend(providerConfig) === 'cliproxy'">
                      <label>backend_provider <span class="req">*</span></label>
                      <input
                        v-model="providerConfig.backend_provider"
                        class="form-input"
                        placeholder="codex"
                      />
                    </template>
                  </template>
                </div>
              </div>

              <label
                >{{ $t("providerDetail.name") }}
                <span class="req">*</span></label
              >
              <input
                v-if="create"
                v-model.trim="providerName"
                class="form-input"
                :placeholder="$t('providerDetail.namePlaceholder')"
              />
              <input v-else :value="providerName" class="form-input" readonly />

              <template v-if="showsURLField">
                <label
                  >{{ providerUrlLabel }} <span class="req">*</span></label
                >
                <div class="field-stack">
                  <input
                    v-model="providerConfig.url"
                    class="form-input"
                    :placeholder="providerUrlPlaceholder(providerConfig)"
                  />
                  <p v-if="providerUrlHint" class="hint">
                    {{ providerUrlHint }}
                  </p>
                </div>
              </template>

              <template v-else>
                <label>{{ $t("providerDetail.connectionSection") }}</label>
                <div class="section-note">
                  {{ connectionNote }}
                </div>
              </template>

              <template v-if="authSourceOptions.length > 0">
                <label>{{ $t("providerDetail.authSource") }}</label>
                <div class="field-stack">
                  <select v-model="authMode" class="form-input">
                    <option
                      v-for="option in authSourceOptions"
                      :key="option.id"
                      :value="option.id"
                    >
                      {{ option.title }}
                    </option>
                  </select>
                  <p v-if="authSourceHint" class="hint">
                    {{ authSourceHint }}
                  </p>
                  <div class="auth-source-details">
                    <template v-if="authMode === 'api_key'">
                      <label class="auth-detail-label">api_key</label>
                      <div class="secret-field">
                        <input
                          :type="showAPIKey ? 'text' : 'password'"
                          :value="secretDisplay(providerConfig.api_key)"
                          @input="
                            apiKeyTouched = true;
                            providerConfig.api_key = $event.target.value;
                          "
                          class="form-input"
                          :placeholder="$t('providerDetail.apiKeyPlaceholder')"
                        />
                        <button
                          class="btn-icon"
                          @click="showAPIKey = !showAPIKey"
                          type="button"
                          :aria-label="$t('providerDetail.toggleApiKeyVisibility')"
                        >
                          {{ showAPIKey ? "🙈" : "👁" }}
                        </button>
                        <span
                          :class="[
                            'badge',
                            isSecretConfigured(providerConfig.api_key)
                              ? 'badge-ok'
                              : 'badge-none',
                          ]"
                        >
                          {{
                            isSecretConfigured(providerConfig.api_key)
                              ? $t("common.configured")
                              : $t("common.notSet")
                          }}
                        </span>
                      </div>
                    </template>

                    <template v-else-if="authMode === 'command'">
                      <label class="auth-detail-label">api_key_command</label>
                      <div class="field-stack">
                        <input
                          v-model="providerConfig.api_key_command"
                          class="form-input"
                          :placeholder="$t('providerDetail.apiKeyCommandPlaceholder')"
                        />
                        <p class="hint">{{ $t("providerDetail.apiKeyCommandHint") }}</p>
                      </div>
                      <div class="auth-command-grid">
                        <div class="field-stack">
                          <label class="auth-detail-label">{{ $t("providerDetail.apiKeyCommandTimeout") }}</label>
                          <input
                            v-model="providerConfig.api_key_command_timeout"
                            class="form-input"
                            placeholder="5s"
                          />
                        </div>
                        <div class="field-stack">
                          <label class="auth-detail-label">{{ $t("providerDetail.apiKeyCommandTTL") }}</label>
                          <input
                            v-model="providerConfig.api_key_command_ttl"
                            class="form-input"
                            placeholder="5m"
                          />
                        </div>
                      </div>
                    </template>

                    <template v-else-if="authMode === 'config_dir'">
                      <label class="auth-detail-label">config_dir</label>
                      <input
                        v-model="providerConfig.config_dir"
                        class="form-input"
                        :placeholder="configDirPlaceholder"
                      />
                    </template>

                    <template v-else>
                      <div class="section-note">
                        {{ authNote }}
                      </div>
                      <template v-if="isManagedCLIProxyAccess">
                        <div class="custom-interface-editor cliproxy-auth-panel">
                          <div class="custom-interface-head">
                            <strong>{{ $t("providerDetail.cliproxyAuthImportSection") }}</strong>
                            <p class="hint">
                              {{ $t("providerDetail.cliproxyAuthImportDesc") }}
                            </p>
                            <p class="hint">
                              {{ $t("providerDetail.cliproxyAuthDir") }}:
                              <code>{{ cliproxyAuthDirLabel }}</code>
                            </p>
                          </div>
                          <div class="cliproxy-auth-import">
                            <label>{{ $t("providerDetail.cliproxyAuthPasteLabel") }}</label>
                            <textarea
                              v-model.trim="cliproxyAuthContent"
                              class="form-input cliproxy-auth-textarea"
                              :placeholder="$t('providerDetail.cliproxyAuthPastePlaceholder')"
                            ></textarea>
                            <div class="cliproxy-auth-file-row">
                              <input
                                ref="cliproxyAuthFileInput"
                                type="file"
                                class="form-input"
                                accept=".json,application/json"
                                @change="handleCliproxyAuthFileSelect"
                              />
                              <input
                                v-model.trim="cliproxyAuthFilename"
                                class="form-input"
                                :placeholder="$t('providerDetail.cliproxyAuthFilenamePlaceholder')"
                              />
                            </div>
                          </div>
                          <div class="actions">
                            <button
                              class="btn btn-primary btn-sm"
                              type="button"
                              :disabled="cliproxyAuthImportDisabled"
                              @click="importCliproxyAuth"
                            >
                              {{ $t("providerDetail.cliproxyAuthUpload") }}
                            </button>
                            <button class="btn btn-secondary btn-sm" type="button" @click="refreshCliproxyAuthFiles">
                              {{ $t("providerDetail.cliproxyAuthRefresh") }}
                            </button>
                            <button class="btn btn-secondary btn-sm" type="button" @click="clearCliproxyAuthDraft">
                              {{ $t("providerDetail.cliproxyAuthClear") }}
                            </button>
                          </div>
                          <div class="auth-file-list">
                            <div class="field-summary">
                              <strong>{{ $t("providerDetail.cliproxyAuthFilesTitle") }}</strong>
                            </div>
                            <div v-if="cliproxyAuthFilesSummary.length > 0" class="auth-files-cards">
                              <div v-for="file in cliproxyAuthFilesSummary" :key="file.filename" class="auth-file-card">
                                <div class="auth-file-card-main">
                                  <code class="auth-file-name">{{ file.filename }}</code>
                                  <div class="auth-file-meta">
                                    <span>{{ file.provider || "-" }}</span>
                                    <span>{{ file.label || "-" }}</span>
                                    <span>{{ file.sizeLabel }}</span>
                                    <span>{{ formatTime(file.modified) }}</span>
                                  </div>
                                </div>
                                <div class="auth-file-status">
                                  <div>
                                    <span class="badge" :class="file.validationClass">{{ file.validationLabel }}</span>
                                    <span v-if="file.validation_message" class="validation-message">
                                      {{ file.validation_message }}
                                    </span>
                                  </div>
                                  <div>
                                    <span v-if="file.onlineResult" class="badge" :class="file.onlineClass">
                                      {{ file.onlineLabel }}
                                    </span>
                                    <span v-if="file.onlineResult?.error" class="validation-message">
                                      {{ file.onlineResult.error }}
                                    </span>
                                    <span v-else-if="file.onlineResult" class="validation-message">
                                      {{ file.onlineResult.model }} / {{ file.onlineResult.latency_ms }}ms
                                    </span>
                                  </div>
                                  <div>
                                    <span v-if="cliproxyAuthUsageLoading[file.filename]" class="badge badge-muted">
                                      {{ $t("providerDetail.cliproxyAuthUsageLoading") }}
                                    </span>
                                    <template v-else-if="file.usageResult">
                                      <span class="badge" :class="file.usageClass">{{ file.usageLabel }}</span>
                                      <span
                                        class="validation-message usage-message"
                                        tabindex="0"
                                        :aria-label="cliproxyAuthUsageDetails(file.usageResult)"
                                      >
                                        <span class="usage-metrics">
                                          <span
                                            v-for="item in cliproxyAuthUsageMetrics(file.usageResult)"
                                            :key="`${file.filename}-${item.name}`"
                                            class="usage-metric"
                                          >
                                            <span class="usage-metric-name">{{ cliproxyAuthUsageMetricName(item.name) }}</span>
                                            <span class="usage-metric-value">{{ item.value }}</span>
                                          </span>
                                          <span v-if="file.usageResult.cached" class="usage-metric usage-metric-muted">
                                            {{ $t("providerDetail.cliproxyAuthUsageCached") }}
                                          </span>
                                        </span>
                                        <pre class="usage-tooltip">{{ cliproxyAuthUsageDetails(file.usageResult) }}</pre>
                                      </span>
                                    </template>
                                  </div>
                                </div>
                                <div class="auth-file-actions">
                                  <button
                                    class="btn btn-secondary btn-sm auth-file-action"
                                    type="button"
                                    :disabled="cliproxyAuthOnlineDisabled(file)"
                                    @click="verifyCliproxyAuth(file)"
                                  >
                                    {{
                                      cliproxyAuthVerifying[file.filename]
                                        ? $t("providerDetail.cliproxyAuthOnlineChecking")
                                        : $t("providerDetail.cliproxyAuthOnlineVerify")
                                    }}
                                  </button>
                                  <button
                                    class="btn btn-danger btn-sm auth-file-action"
                                    type="button"
                                    :disabled="cliproxyAuthDeleting[file.filename]"
                                    @click="deleteCliproxyAuth(file)"
                                  >
                                    {{
                                      cliproxyAuthDeleting[file.filename]
                                        ? $t("providerDetail.cliproxyAuthDeleting")
                                        : $t("common.delete")
                                    }}
                                  </button>
                                </div>
                              </div>
                            </div>
                            <div v-else class="section-note">
                              {{ $t("providerDetail.cliproxyAuthFilesEmpty") }}
                            </div>
                          </div>
                        </div>
                      </template>
                    </template>
                  </div>
                </div>
              </template>

              <label>{{ $t("providerDetail.availableInterfaces") }}</label>
              <div class="field-stack">
                <select
                  :value="selectedServiceTemplateId"
                  class="form-input"
                  @change="handleServiceTemplateChange($event.target.value)"
                >
                  <option
                    v-for="template in capabilityTemplateOptions"
                    :key="template.id"
                    :value="template.id"
                  >
                    {{ serviceTemplateTitle(template) }}
                  </option>
                </select>
                <p class="hint">{{ currentServiceTemplateSummary }}</p>
              </div>

              <div class="form-grid-full interface-preview">
                <span class="interface-preview-title">
                  {{ $t("providerDetail.finalInterfaces") }}
                </span>
                <div class="protocol-chip-list">
                  <span
                    v-for="protocol in effectiveServiceProtocols"
                    :key="protocol"
                    class="badge badge-muted"
                  >
                    {{ serviceProtocolTitle(protocol) }}
                  </span>
                  <span
                    v-if="effectiveServiceProtocols.length === 0"
                    class="hint"
                  >
                    {{ $t("providerDetail.noEffectiveProtocols") }}
                  </span>
                </div>
                <p class="hint">
                  {{ $t("providerDetail.finalInterfacesHint") }}
                </p>
              </div>

              <div
                v-if="isCustomServiceTemplate"
                class="form-grid-full custom-interface-editor"
              >
                <div class="custom-interface-head">
                  <span class="interface-preview-title">
                    {{ $t("providerDetail.customInterfacesSection") }}
                  </span>
                  <span class="hint">
                    {{ $t("providerDetail.customInterfacesDesc") }}
                  </span>
                </div>
                <div class="form-grid compact-grid">
                  <label>{{ $t("providerDetail.rawServiceProtocols") }}</label>
                  <div class="service-protocols-editor">
                    <TagListEditor
                      v-model="providerConfig.service_protocols"
                      :suggestions="serviceProtocolSuggestions"
                      :placeholder="
                        $t('providerDetail.serviceProtocolsPlaceholder')
                      "
                    />
                    <p class="hint service-protocols-hint">
                      {{ $t("providerDetail.serviceProtocolsHint") }}
                    </p>
                  </div>

                  <template v-if="providerFamily(providerConfig) === 'openai'">
                    <label>responses_to_chat</label>
                    <div class="form-hint-row">
                      <input
                        type="checkbox"
                        v-model="providerConfig.responses_to_chat"
                        class="form-checkbox"
                      />
                      <span class="hint">{{
                        $t("config.responsesToChatHint")
                      }}</span>
                    </div>

                    <label>anthropic_to_chat</label>
                    <div class="form-hint-row">
                      <input
                        type="checkbox"
                        v-model="providerConfig.anthropic_to_chat"
                        class="form-checkbox"
                      />
                      <span class="hint">{{
                        $t("config.anthropicToChatHint")
                      }}</span>
                    </div>
                  </template>
                </div>
              </div>
            </div>
          </section>

          <section
            v-if="providerFamily(providerConfig)"
            class="form-panel optional-panel"
          >
            <div class="form-panel-head">
              <div>
                <h4>
                  {{ $t("providerDetail.staticModelsSection") }}
                  <span class="summary-count">
                    {{
                      $t("providerDetail.modelsConfiguredCount", {
                        n: providerConfig.models.length,
                      })
                    }}
                  </span>
                </h4>
              </div>
            </div>
            <p class="section-desc advanced-desc">
              {{ $t("providerDetail.staticModelsSectionDesc") }}
            </p>
            <div class="models-editor">
              <div class="models-toolbar">
                <div class="models-meta">
                  <span class="badge badge-none">
                    {{ $t("providerDetail.modelsOptional") }}
                  </span>
                  <span v-if="!create" class="hint">
                    {{
                      $t("providerDetail.modelsDiscoveredCount", {
                        n: discoveredModelIds.length,
                      })
                    }}
                  </span>
                </div>
                <button
                  v-if="missingDiscoveredModelIds.length > 0"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="appendDiscoveredModels"
                >
                  {{
                    $t("providerDetail.addDiscoveredModels", {
                      n: missingDiscoveredModelIds.length,
                    })
                  }}
                </button>
              </div>
              <p class="hint models-hint">
                {{
                  discoveredModelIds.length > 0
                    ? $t("providerDetail.modelsSuggestionHint", {
                        n: discoveredModelIds.length,
                      })
                    : $t("providerDetail.modelsNoSuggestionHint")
                }}
              </p>
              <TagListEditor
                v-model="providerConfig.models"
                :suggestions="discoveredModelIds"
                :placeholder="$t('providerDetail.modelsPlaceholder')"
              />
              <p class="hint models-hint">
                {{ $t("providerDetail.modelsBehaviorHint") }}
              </p>
            </div>
          </section>

          <section class="form-panel advanced-panel">
            <div class="form-panel-head">
              <div>
                <h4>{{ $t("providerDetail.advancedSection") }}</h4>
              </div>
            </div>
            <p class="section-desc advanced-desc">
              {{ $t("providerDetail.advancedSectionDesc") }}
            </p>
            <div class="form-grid">
              <template v-if="showsHeadersField">
                <label>headers</label>
                <KeyValueEditor
                  v-model="providerConfig.headers"
                  keyPlaceholder="Header name"
                  valuePlaceholder="Value"
                />
              </template>

              <label>proxy</label>
              <input
                v-model="providerConfig.proxy"
                class="form-input"
                :placeholder="$t('providerDetail.proxyPlaceholder')"
              />

              <label>{{ $t("providerDetail.timeout") }}</label>
              <input
                v-model="providerConfig.timeout"
                class="form-input"
                :placeholder="$t('providerDetail.defaultTimeout')"
              />
            </div>
          </section>
        </div>
      </section>

      <div v-if="!create && detail" class="runtime-stack">
        <section class="info-section">
          <div class="section-top compact-top">
            <div>
              <h3>{{ $t("providerDetail.runtimeTools") }}</h3>
              <p class="section-desc">
                {{ $t("providerDetail.runtimeToolsDesc") }}
              </p>
            </div>
            <div class="actions runtime-actions">
              <button
                @click="runHealthCheck"
                class="btn btn-primary"
                :disabled="checking"
              >
                {{
                  checking
                    ? $t("providerDetail.checking")
                    : $t("providerDetail.healthCheck")
                }}
              </button>
              <button
                v-if="detail.status && !detail.status.manual_suppressed"
                @click="suppressProvider"
                class="btn btn-danger"
              >
                {{ $t("providerDetail.suppressBtn") }}
              </button>
              <button
                v-else-if="detail.status"
                @click="unsuppressProvider"
                class="btn btn-secondary"
              >
                {{ $t("providerDetail.unsuppressBtn") }}
              </button>
            </div>
          </div>
          <span
            v-if="healthResult"
            class="health-result"
            :class="
              healthResult.status === 'ok' ? 'text-success' : 'text-error'
            "
          >
            {{
              healthResult.status === "ok"
                ? $t("providerDetail.healthOk", {
                    latency: healthResult.latency_ms,
                    count: healthResult.model_count,
                  })
                : $t("providerDetail.healthError", {
                    error: healthResult.error,
                  })
            }}
          </span>
        </section>

        <details class="info-section runtime-panel" open>
          <summary>{{ $t("providerDetail.runtimeOverview") }}</summary>
          <div class="runtime-tables">
            <table class="info-table">
              <tr>
                <td>{{ $t("providerDetail.name") }}</td>
                <td>{{ detail.name }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.providerType") }}</td>
                <td>{{ runtimeAccessTypeTitle }}</td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.url") }}</td>
                <td>
                  <code>{{ detail.url }}</code>
                </td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.family") }}</td>
                <td>{{ detail.family || detail.protocol }}</td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy && detail.backend">
                <td>backend</td>
                <td>{{ detail.backend }}</td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy && detail.backend_provider">
                <td>backend_provider</td>
                <td>{{ detail.backend_provider }}</td>
              </tr>
              <tr v-if="detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.connectionSection") }}</td>
                <td>{{ $t("providerDetail.cliproxyConnectionNote") }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.configuredProtocols") }}</td>
                <td>
                  {{ (detail.configured_protocols || []).join(", ") || "-" }}
                </td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.displayProtocols") }}</td>
                <td>
                  {{ (detail.display_protocols || []).join(", ") || "-" }}
                </td>
              </tr>
              <tr v-if="detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.authSourceRuntime") }}</td>
                <td>{{ $t("providerDetail.authSourceCLIProxyAuthDir") }}</td>
              </tr>
              <tr v-else>
                <td>{{ $t("providerDetail.authSourceRuntime") }}</td>
                <td>{{ runtimeAuthSourceTitle }}</td>
              </tr>
            </table>

            <table v-if="detail.status" class="info-table">
              <tr>
                <td>{{ $t("providerDetail.suppressed") }}</td>
                <td>
                  {{
                    detail.status.suppressed
                      ? $t("providerDetail.yes")
                      : $t("providerDetail.no")
                  }}
                </td>
              </tr>
              <tr v-if="detail.status.suppressed">
                <td>{{ $t("providerDetail.suppressedUntil") }}</td>
                <td>{{ formatTime(detail.status.suppress_until) }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.consecutiveFailures") }}</td>
                <td>{{ detail.status.consecutive_failures }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.totalRequests") }}</td>
                <td>{{ detail.status.total_requests }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.success") }}</td>
                <td>{{ detail.status.success_count }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.failure") }}</td>
                <td>{{ detail.status.failure_count }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.avgLatency") }}</td>
                <td>
                  {{
                    detail.status.total_requests > 0
                      ? detail.status.avg_latency_ms.toFixed(0) + "ms"
                      : "-"
                  }}
                </td>
              </tr>
            </table>
          </div>
        </details>

        <details class="info-section runtime-panel">
          <summary>
            {{
              $t("providerDetail.availableModels", { n: detail.models.length })
            }}
          </summary>
          <div v-if="detail.models.length === 0" class="empty">
            {{ $t("providerDetail.noModels") }}
          </div>
          <table v-else class="data-table">
            <thead>
              <tr>
                <th>{{ $t("providerDetail.modelId") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in parsedModels" :key="m.id">
                <td>
                  <code>{{ m.id }}</code>
                </td>
              </tr>
            </tbody>
          </table>
        </details>

        <details class="info-section runtime-panel">
          <summary>{{ $t("providerDetail.protocolDetection") }}</summary>
          <div class="runtime-actions">
            <button
              @click="runProtocolDetect"
              class="btn btn-secondary"
              :disabled="detectingProtocols"
            >
              {{
                detectingProtocols
                  ? $t("providerDetail.detecting")
                  : $t("providerDetail.detectDisplayProtocols")
              }}
            </button>
            <span v-if="detail.last_protocol_probe" class="hint">
              {{ detail.last_protocol_probe.status }} ·
              {{ formatTime(detail.last_protocol_probe.checked_at) }}
              <span v-if="detail.last_protocol_probe.error">
                · {{ detail.last_protocol_probe.error }}</span
              >
            </span>
          </div>

          <div class="probe-grid">
            <select v-model="selectedProbeModel" class="form-input">
              <option value="">{{ $t("providerDetail.selectModel") }}</option>
              <option
                v-for="model in probeableModels"
                :key="model"
                :value="model"
              >
                {{ model }}
              </option>
            </select>
            <select v-model="selectedProbeProtocol" class="form-input">
              <option value="chat">chat</option>
              <option value="responses_stateless">responses_stateless</option>
              <option value="responses_stateful">responses_stateful</option>
              <option value="anthropic">anthropic</option>
            </select>
            <button
              @click="runExactProtocolProbe"
              class="btn btn-primary"
              :disabled="exactProbing || !selectedProbeModel"
            >
              {{
                exactProbing
                  ? $t("providerDetail.probing")
                  : $t("providerDetail.probeModelProtocol")
              }}
            </button>
          </div>

          <div
            v-if="protocolProbeResult"
            class="hint"
            :class="
              protocolProbeResult.status === 'supported'
                ? 'text-success'
                : protocolProbeResult.status === 'unsupported'
                  ? 'text-error'
                  : 'text-warning'
            "
          >
            {{ protocolProbeResult.model }} ·
            {{ protocolProbeResult.protocol }} ·
            {{ protocolProbeResult.status }}
            <span v-if="protocolProbeResult.error">
              · {{ protocolProbeResult.error }}</span
            >
          </div>

          <table
            v-if="exactProbeResults.length > 0"
            class="data-table probe-results-table"
          >
            <thead>
              <tr>
                <th>{{ $t("providerDetail.probeColModel") }}</th>
                <th>{{ $t("providerDetail.probeColProtocol") }}</th>
                <th>{{ $t("providerDetail.probeColStatus") }}</th>
                <th>{{ $t("providerDetail.probeColCheckedAt") }}</th>
                <th>{{ $t("providerDetail.probeColError") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="probe in exactProbeResults"
                :key="`${probe.model}/${probe.protocol}`"
              >
                <td>
                  <code>{{ probe.model }}</code>
                </td>
                <td>
                  <code>{{ probe.protocol }}</code>
                </td>
                <td>{{ probe.status }}</td>
                <td>{{ formatTime(probe.checked_at) }}</td>
                <td>{{ probe.error || "-" }}</td>
              </tr>
            </tbody>
          </table>
        </details>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
  createCLIProxyAuthFile,
  deleteCLIProxyAuthFile,
  detectProviderProtocols,
  fetchConfig,
  fetchConfigSource,
  fetchCLIProxyAuthFileUsage,
  fetchCLIProxyAuthFiles,
  fetchProviderDetail,
  fetchProviderFormMeta,
  fetchStatus,
  healthCheck,
  probeProviderModelProtocol,
  restartGateway,
  saveConfig,
  setProviderSuppress,
  validateConfig,
  verifyCLIProxyAuthFile,
} from "../api.js";
import KeyValueEditor from "../components/KeyValueEditor.vue";
import TagListEditor from "../components/TagListEditor.vue";
import {
  cleanConfig,
  cloneData,
  defaultServiceProtocolsForProvider,
  normalizeLowerText,
  normalizeServiceProtocols,
  providerBackend,
  providerFamily,
  serviceProtocolsEqual,
} from "../config-utils.js";
import { bindPollState, pollUntilAlive } from "../runtime-utils.js";

const { t } = useI18n();
const router = useRouter();

const REDACTED = "__REDACTED__";
const CLEAR_API_KEY_MARKER = "__clear_api_key__";
const CUSTOM_ACCESS_TYPE = "__custom_access__";
const CUSTOM_SERVICE_TEMPLATE = "__custom__";

const props = defineProps({
  name: { type: String, default: "" },
  create: { type: Boolean, default: false },
});

const detail = ref(null);
const configDoc = ref({});
const configSource = ref(null);
const providerFormMeta = ref({ presets: [], service_protocol_templates: [] });
const providerName = ref("");
const providerConfig = ref(createEmptyProviderConfig());
const selectedPresetId = ref("");
const selectedServiceTemplateId = ref(CUSTOM_SERVICE_TEMPLATE);
const selectedAuthSource = ref("");
const error = ref("");
const message = ref("");
const checking = ref(false);
const healthResult = ref(null);
const saving = ref(false);
const loading = ref(false);
const dirty = ref(false);
const suppressDirty = ref(false);
const showAPIKey = ref(false);
const apiKeyTouched = ref(false);
const configFileChanged = ref(false);
const waitingAlive = ref(false);
const waitingElapsed = ref(0);
const detectingProtocols = ref(false);
const cliproxyAuthFiles = ref([]);
const cliproxyAuthLoading = ref(false);
const cliproxyAuthContent = ref("");
const cliproxyAuthFilename = ref("");
const cliproxyAuthFileInput = ref(null);
const cliproxyAuthVerifying = ref({});
const cliproxyAuthDeleting = ref({});
const cliproxyAuthOnlineResults = ref({});
const cliproxyAuthUsageLoading = ref({});
const cliproxyAuthUsageResults = ref({});
const cliproxyAuthUsageRequestID = ref(0);
const selectedProbeModel = ref("");
const selectedProbeProtocol = ref("chat");
const protocolProbeResult = ref(null);
const exactProbing = ref(false);

watch(
  [providerName, providerConfig],
  () => {
    if (!suppressDirty.value) dirty.value = true;
  },
  { deep: true },
);

watch(
  () => [
    providerConfig.value.family,
    providerConfig.value.backend,
    providerConfig.value.backend_provider,
    providerConfig.value.url,
    providerConfig.value.config_dir,
    providerConfig.value.api_key_command,
    providerConfig.value.service_protocols,
    providerConfig.value.anthropic_to_chat,
  ],
  () => {
    if (selectedPresetId.value !== CUSTOM_ACCESS_TYPE) {
      selectedPresetId.value =
        inferPresetID(providerConfig.value) || CUSTOM_ACCESS_TYPE;
    }
    if (selectedServiceTemplateId.value !== CUSTOM_SERVICE_TEMPLATE) {
      syncSelectedServiceTemplate();
    }
  },
  { deep: true },
);

watch(
  () => [props.name, props.create],
  () => {
    load();
  },
  { immediate: true },
);

const pageTitle = computed(() =>
  props.create
    ? t("providerDetail.newProviderTitle")
    : providerName.value || props.name,
);

const providerPresets = computed(() => providerFormMeta.value?.presets || []);
const serviceProtocolTemplates = computed(
  () => providerFormMeta.value?.service_protocol_templates || [],
);
const accessTypeOptions = computed(() => [
  ...providerPresets.value,
  {
    id: CUSTOM_ACCESS_TYPE,
    title: t("providerDetail.customAccessType"),
    summary: t("providerDetail.customAccessTypeDesc"),
  },
]);

const currentPreset = computed(
  () =>
    providerPresets.value.find(
      (preset) => preset.id === selectedPresetId.value,
    ) || null,
);
const selectedAccessTypeId = computed(() =>
  currentPreset.value ? currentPreset.value.id : CUSTOM_ACCESS_TYPE,
);
const isCustomAccessType = computed(
  () => selectedAccessTypeId.value === CUSTOM_ACCESS_TYPE,
);
const isCLIProxyBackend = computed(
  () => providerBackend(providerConfig.value) === "cliproxy",
);
const isManagedCLIProxyAccess = computed(
  () => isCLIProxyBackend.value && !isCustomAccessType.value,
);

watch(
  isManagedCLIProxyAccess,
  async (enabled) => {
    if (enabled) {
      await refreshCliproxyAuthFiles();
    } else {
      invalidateCliproxyAuthUsageRequests();
      cliproxyAuthFiles.value = [];
      cliproxyAuthUsageResults.value = {};
      cliproxyAuthUsageLoading.value = {};
      clearCliproxyAuthDraft();
    }
  },
  { immediate: true },
);

const currentAccessTypeSummary = computed(() => {
  const current = accessTypeOptions.value.find(
    (option) => option.id === selectedAccessTypeId.value,
  );
  return accessTypeSummary(current);
});

const visibleServiceProtocolTemplates = computed(() => {
  const family = providerFamily(providerConfig.value);
  const backend = providerBackend(providerConfig.value);
  return serviceProtocolTemplates.value.filter((template) => {
    if (
      Array.isArray(template.families) &&
      template.families.length > 0 &&
      !template.families.includes(family)
    ) {
      return false;
    }
    if (
      Array.isArray(template.backends) &&
      template.backends.length > 0 &&
      !template.backends.includes(backend)
    ) {
      return false;
    }
    return true;
  });
});

const capabilityTemplateOptions = computed(() => [
  ...visibleServiceProtocolTemplates.value,
  {
    id: CUSTOM_SERVICE_TEMPLATE,
    title: t("providerDetail.interfaceTemplateCustom"),
    summary: t("providerDetail.interfaceTemplateCustomDesc"),
  },
]);

const currentServiceTemplateSummary = computed(() => {
  const current = capabilityTemplateOptions.value.find(
    (template) => template.id === selectedServiceTemplateId.value,
  );
  return serviceTemplateSummary(current);
});

const isCustomServiceTemplate = computed(
  () => selectedServiceTemplateId.value === CUSTOM_SERVICE_TEMPLATE,
);

const parsedModels = computed(() => {
  if (!detail.value) return [];
  return detail.value.models.map((model) => {
    if (typeof model === "string") {
      try {
        return JSON.parse(model);
      } catch {
        return { id: model };
      }
    }
    return model;
  });
});

const discoveredModelIds = computed(() => {
  const ids = [];
  for (const model of parsedModels.value) {
    const id = typeof model?.id === "string" ? model.id.trim() : "";
    if (!id || ids.includes(id)) continue;
    ids.push(id);
  }
  return ids;
});

const missingDiscoveredModelIds = computed(() =>
  discoveredModelIds.value.filter(
    (id) => !(providerConfig.value.models || []).includes(id),
  ),
);

const probeableModels = computed(() =>
  discoveredModelIds.value.length > 0
    ? discoveredModelIds.value
    : [...(providerConfig.value.models || [])],
);

const exactProbeResults = computed(() => {
  const entries = detail.value?.model_protocol_probes || [];
  return [...entries].sort((a, b) => {
    const modelCmp = String(a.model || "").localeCompare(String(b.model || ""));
    if (modelCmp !== 0) return modelCmp;
    return String(a.protocol || "").localeCompare(String(b.protocol || ""));
  });
});

const detailIsManagedCLIProxy = computed(
  () => providerBackend(detail.value) === "cliproxy",
);

const runtimeAccessTypeTitle = computed(() => {
  if (!detail.value) return "-";
  const presetID = inferPresetID(detail.value);
  const preset = providerPresets.value.find((item) => item.id === presetID);
  return preset?.title || detail.value.family || detail.value.protocol || "-";
});

const runtimeAuthSourceTitle = computed(() =>
  authSourceTitle(detail.value?.auth_source || inferAuthSource(detail.value)),
);

const effectiveServiceProtocols = computed(() => {
  const configured = normalizeServiceProtocols(
    providerConfig.value.service_protocols,
  );
  if (configured.length > 0) return configured;
  return defaultServiceProtocolsForProvider(providerConfig.value);
});

const showsURLField = computed(
  () =>
    !!providerFamily(providerConfig.value) &&
    !["copilot"].includes(providerFamily(providerConfig.value)) &&
    !isManagedCLIProxyAccess.value,
);

const showsHeadersField = computed(() => showsURLField.value);

const connectionNote = computed(() =>
  isManagedCLIProxyAccess.value
    ? t("providerDetail.cliproxyConnectionNote")
    : t("providerDetail.noUrlRequired"),
);

const authNote = computed(() =>
  isManagedCLIProxyAccess.value
    ? t("providerDetail.cliproxyAuthNote")
    : t("providerDetail.authManagedByBackend"),
);

const providerUrlLabel = computed(() =>
  isCLIProxyBackend.value ? t("providerDetail.cliproxyEndpoint") : "url",
);

const providerUrlHint = computed(() =>
  isCLIProxyBackend.value ? t("providerDetail.cliproxyEndpointHint") : "",
);

const authSourceOptions = computed(() => {
  const family = providerFamily(providerConfig.value);
  if (!family) return [];
  if (providerBackend(providerConfig.value) === "cliproxy") {
    return [{ id: "none", title: t("providerDetail.authSourceCLIProxyAuthDir") }];
  }
  if (family === "copilot") {
    return [
      { id: "config_dir", title: authSourceTitle("config_dir") },
      { id: "api_key", title: authSourceTitle("api_key") },
      { id: "command", title: authSourceTitle("command") },
      { id: "none", title: authSourceTitle("none") },
    ];
  }
  return [
    { id: "api_key", title: authSourceTitle("api_key") },
    { id: "command", title: authSourceTitle("command") },
    { id: "none", title: authSourceTitle("none") },
  ];
});

const authMode = computed({
  get() {
    const available = authSourceOptions.value.map((option) => option.id);
    if (available.includes(selectedAuthSource.value)) {
      return selectedAuthSource.value;
    }
    const inferred = inferAuthSource(providerConfig.value);
    if (available.includes(inferred)) {
      return inferred;
    }
    return available[0] || "";
  },
  set(value) {
    selectedAuthSource.value = value;
  },
});

const authSourceHint = computed(() => {
  if (isManagedCLIProxyAccess.value) {
    return t("providerDetail.cliproxyAuthNote");
  }
  switch (authMode.value) {
    case "command":
      return t("providerDetail.apiKeyCommandSecurityHint");
    case "config_dir":
      return t("providerDetail.configDirAuthHint");
    case "none":
      return t("providerDetail.noAuthHint");
    default:
      return "";
  }
});

const cliproxyAuthDirLabel = computed(() => {
  const dir = configDoc.value?.cliproxy?.auth_dir || "/etc/warden";
  return dir;
});

const cliproxyAuthFilesSummary = computed(() =>
  cliproxyAuthFiles.value.map((file) => ({
    ...file,
    sizeLabel: formatBytes(file.size),
    validationLabel: cliproxyAuthValidationLabel(file.validation_status),
    validationClass: cliproxyAuthValidationClass(file.validation_status),
    onlineResult: cliproxyAuthOnlineResults.value[file.filename] || null,
    onlineLabel: cliproxyAuthOnlineLabel(cliproxyAuthOnlineResults.value[file.filename]?.status),
    onlineClass: cliproxyAuthOnlineClass(cliproxyAuthOnlineResults.value[file.filename]?.status),
    usageResult: cliproxyAuthUsageResults.value[file.filename] || null,
    usageLabel: cliproxyAuthUsageLabel(cliproxyAuthUsageResults.value[file.filename]?.status),
    usageClass: cliproxyAuthUsageClass(cliproxyAuthUsageResults.value[file.filename]?.status),
  })),
);

const configDirPlaceholder = computed(() => {
  if (currentPreset.value?.default_config_dir)
    return currentPreset.value.default_config_dir;
  switch (providerFamily(providerConfig.value)) {
    case "copilot":
      return "~/.config/github-copilot";
    default:
      return "";
  }
});

const cliproxyAuthImportDisabled = computed(
  () =>
    !isManagedCLIProxyAccess.value ||
    cliproxyAuthLoading.value ||
    !String(cliproxyAuthContent.value || "").trim(),
);

const serviceProtocolSuggestions = computed(() => {
  if (providerBackend(providerConfig.value) === "cliproxy") {
    return ["chat", "responses_stateless", "responses_stateful"];
  }
  switch (providerFamily(providerConfig.value)) {
    case "openai": {
      const protocols = [
        "chat",
        "responses_stateless",
        "responses_stateful",
        "embeddings",
      ];
      if (providerConfig.value?.anthropic_to_chat) protocols.push("anthropic");
      return protocols;
    }
    case "anthropic":
      return ["chat", "anthropic"];
    case "copilot":
      return ["chat"];
    default:
      return [];
  }
});

function createEmptyProviderConfig() {
  return {
    url: "",
    family: "",
    backend: "",
    backend_provider: "",
    service_protocols: [],
    models: [],
    responses_to_chat: false,
    anthropic_to_chat: false,
    proxy: "",
    timeout: "",
    config_dir: "",
    headers: {},
    api_key: "",
    api_key_command: "",
    api_key_command_timeout: "",
    api_key_command_ttl: "",
  };
}

function inferAuthSource(provider) {
  if (!providerFamily(provider)) return "";
  if (providerBackend(provider) === "cliproxy") return "none";
  if (String(provider?.api_key_command || "").trim()) return "command";
  if (provider?.api_key) return "api_key";
  if (providerFamily(provider) === "copilot") return "config_dir";
  return "api_key";
}

function authSourceTitle(source) {
  switch (source) {
    case "api_key":
      return t("providerDetail.authSourceStatic");
    case "command":
      return t("providerDetail.authSourceCommand");
    case "config_dir":
      return t("providerDetail.authSourceConfigDir");
    case "none":
      return t("providerDetail.authSourceNone");
    default:
      return source || "-";
  }
}

function applyProviderAuthSource(provider, source) {
  delete provider.api_key_command;
  delete provider.api_key_command_timeout;
  delete provider.api_key_command_ttl;
  delete provider.config_dir;

  switch (source) {
    case "api_key":
      if (!apiKeyTouched.value) {
        if (provider.api_key === REDACTED) {
          provider.api_key = REDACTED;
          return;
        }
        delete provider.api_key;
        return;
      }
      provider.api_key = String(provider.api_key || "");
      return;
    case "command":
      delete provider.api_key;
      provider.api_key_command = String(providerConfig.value.api_key_command || "").trim();
      if (String(providerConfig.value.api_key_command_timeout || "").trim()) {
        provider.api_key_command_timeout = String(
          providerConfig.value.api_key_command_timeout,
        ).trim();
      }
      if (String(providerConfig.value.api_key_command_ttl || "").trim()) {
        provider.api_key_command_ttl = String(
          providerConfig.value.api_key_command_ttl,
        ).trim();
      }
      return;
    case "config_dir":
      delete provider.api_key;
      provider.config_dir = providerConfig.value.config_dir || "";
      return;
    case "none":
      delete provider.api_key;
      provider[CLEAR_API_KEY_MARKER] = true;
      return;
    default:
      delete provider.api_key;
  }
}

function inferPresetID(provider) {
  const family = providerFamily(provider);
  const backend = providerBackend(provider);
  const backendProvider = normalizeLowerText(provider?.backend_provider);
  const url = String(provider?.url || "").trim();
  if (family === "openai" && backend === "cliproxy" && backendProvider) {
    const match = providerPresets.value.find(
      (preset) =>
        preset.family === family &&
        normalizeLowerText(preset.backend) === backend &&
        normalizeLowerText(preset.backend_provider) === backendProvider,
    );
    return match?.id || "";
  }
  if (family === "openai" && backend === "") {
    if (url === "http://127.0.0.1:11434/v1") return "ollama-chat";
    return "openai-compatible";
  }
  if (family === "anthropic" && url === "https://api.anthropic.com/v1")
    return "anthropic-official";
  if (family === "copilot") return "copilot-cli";
  return "";
}

function inferServiceTemplateID(provider) {
  if (provider.responses_to_chat) return CUSTOM_SERVICE_TEMPLATE;
  for (const template of visibleServiceProtocolTemplates.value) {
    if (
      !serviceProtocolsEqual(
        provider.service_protocols,
        template.service_protocols,
      )
    )
      continue;
    if (!!provider.anthropic_to_chat !== !!template.anthropic_to_chat) continue;
    return template.id;
  }
  return CUSTOM_SERVICE_TEMPLATE;
}

function syncSelectedServiceTemplate() {
  selectedServiceTemplateId.value = inferServiceTemplateID(
    providerConfig.value,
  );
}

function accessTypeTitle(option) {
  return option?.title || "";
}

function accessTypeSummary(option) {
  if (!option?.id) return "";
  if (option.id === CUSTOM_ACCESS_TYPE) {
    return option.summary || t("providerDetail.customAccessTypeDesc");
  }
  return option.summary || "";
}

function serviceTemplateTitle(template) {
  if (!template?.id) return "";
  if (template.id === CUSTOM_SERVICE_TEMPLATE) return template.title || "";
  const key = `providerDetail.interfaceTemplate_${template.id}`;
  const translated = t(key);
  return translated === key ? template.title || template.id : translated;
}

function serviceTemplateSummary(template) {
  if (!template?.id) return t("providerDetail.interfaceTemplateCustomDesc");
  if (template.id === CUSTOM_SERVICE_TEMPLATE) {
    return template.summary || t("providerDetail.interfaceTemplateCustomDesc");
  }
  const key = `providerDetail.interfaceTemplate_${template.id}_desc`;
  const translated = t(key);
  return translated === key ? template.summary || "" : translated;
}

function serviceProtocolTitle(protocol) {
  const key = `providerDetail.serviceProtocol_${protocol}`;
  const translated = t(key);
  return translated === key ? protocol : translated;
}

function applyServiceProtocolTemplateByID(templateID) {
  const template = serviceProtocolTemplates.value.find(
    (item) => item.id === templateID,
  );
  if (!template) return;
  providerConfig.value.service_protocols = [
    ...(template.service_protocols || []),
  ];
  providerConfig.value.anthropic_to_chat = !!template.anthropic_to_chat;
  selectedServiceTemplateId.value = templateID;
}

function applyPresetByID(presetID) {
  const preset = providerPresets.value.find((item) => item.id === presetID);
  if (!preset) return;

  const current = providerConfig.value || createEmptyProviderConfig();
  const previousPreset = currentPreset.value;
  const next = createEmptyProviderConfig();
  next.family = preset.family || "";
  next.backend = preset.backend || "";
  next.backend_provider = preset.backend_provider || "";
  next.url = presetFieldValue(
    current.url,
    previousPreset?.default_url,
    preset.default_url,
  );
  next.config_dir = presetFieldValue(
    current.config_dir,
    previousPreset?.default_config_dir,
    preset.default_config_dir,
  );
  next.models = [...(current.models || [])];
  next.headers = cloneData(current.headers || {});
  next.proxy = current.proxy || "";
  next.timeout = current.timeout || "";
  next.api_key = current.api_key || "";
  next.api_key_command = current.api_key_command || "";
  next.api_key_command_timeout = current.api_key_command_timeout || "";
  next.api_key_command_ttl = current.api_key_command_ttl || "";
  providerConfig.value = next;
  selectedPresetId.value = preset.id;
  selectedAuthSource.value = inferAuthSource(next);
  showAPIKey.value = false;
  if (preset.service_protocol_template) {
    applyServiceProtocolTemplateByID(preset.service_protocol_template);
  } else {
    syncSelectedServiceTemplate();
  }
}

function applyAccessPresetByID(presetID) {
  const preset = providerPresets.value.find((item) => item.id === presetID);
  if (!preset) return;

  const previousPreset = currentPreset.value;
  providerConfig.value.family = preset.family || "";
  providerConfig.value.backend = preset.backend || "";
  providerConfig.value.backend_provider = preset.backend_provider || "";
  providerConfig.value.url = presetFieldValue(
    providerConfig.value.url,
    previousPreset?.default_url,
    preset.default_url,
  );
  providerConfig.value.config_dir = presetFieldValue(
    providerConfig.value.config_dir,
    previousPreset?.default_config_dir,
    preset.default_config_dir,
  );
  selectedPresetId.value = preset.id;
  selectedAuthSource.value = inferAuthSource(providerConfig.value);
  if (preset.service_protocol_template) {
    applyServiceProtocolTemplateByID(preset.service_protocol_template);
  } else {
    syncSelectedServiceTemplate();
  }
}

function handleAccessTypeChange(presetID) {
  if (presetID === CUSTOM_ACCESS_TYPE) {
    selectedPresetId.value = CUSTOM_ACCESS_TYPE;
    return;
  }
  if (props.create) {
    applyPresetByID(presetID);
    return;
  }
  applyAccessPresetByID(presetID);
}

function presetFieldValue(currentValue, previousDefault, nextDefault) {
  const current = String(currentValue || "").trim();
  const prev = String(previousDefault || "").trim();
  const next = String(nextDefault || "").trim();
  if (!current) return next;
  if (prev && current === prev) return next;
  return currentValue || "";
}

function handleServiceTemplateChange(templateID) {
  selectedServiceTemplateId.value = templateID;
  if (templateID === CUSTOM_SERVICE_TEMPLATE) return;
  applyServiceProtocolTemplateByID(templateID);
}

function secretDisplay(value) {
  return value === REDACTED ? REDACTED : value || "";
}

function isSecretConfigured(value) {
  return !!value;
}

function providerUrlPlaceholder(provider) {
  if (providerBackend(provider) === "cliproxy") {
    return currentPreset.value?.default_url || "http://127.0.0.1:18741/v1";
  }
  switch (providerFamily(provider)) {
    case "":
      return "Select family first";
    case "anthropic":
      return "https://api.anthropic.com/v1";
    case "copilot":
      return "https://api.githubcopilot.com";
    default:
      return "https://api.openai.com/v1";
  }
}

function appendDiscoveredModels() {
  if (missingDiscoveredModelIds.value.length === 0) return;
  providerConfig.value.models = [
    ...(providerConfig.value.models || []),
    ...missingDiscoveredModelIds.value,
  ];
}

function formatTime(timeValue) {
  if (!timeValue) return "";
  return new Date(timeValue).toLocaleString();
}

function formatBytes(size) {
  if (!Number.isFinite(size) || size < 0) return "-";
  if (size < 1024) return `${size} B`;
  const units = ["KB", "MB", "GB"];
  let value = size / 1024;
  let unit = units[0];
  for (let i = 1; i < units.length && value >= 1024; i += 1) {
    value /= 1024;
    unit = units[i];
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${unit}`;
}

function cliproxyAuthValidationLabel(status) {
  switch (String(status || "").toLowerCase()) {
    case "valid":
      return t("providerDetail.cliproxyAuthValidationValid");
    case "warning":
      return t("providerDetail.cliproxyAuthValidationWarning");
    case "invalid":
      return t("providerDetail.cliproxyAuthValidationInvalid");
    default:
      return t("providerDetail.cliproxyAuthValidationUnknown");
  }
}

function cliproxyAuthValidationClass(status) {
  switch (String(status || "").toLowerCase()) {
    case "valid":
      return "badge-ok";
    case "warning":
      return "badge-warn";
    case "invalid":
      return "badge-error";
    default:
      return "badge-muted";
  }
}

function cliproxyAuthOnlineLabel(status) {
  switch (String(status || "").toLowerCase()) {
    case "ok":
      return t("providerDetail.cliproxyAuthOnlineOk");
    case "error":
      return t("providerDetail.cliproxyAuthOnlineError");
    default:
      return t("providerDetail.cliproxyAuthValidationUnknown");
  }
}

function cliproxyAuthOnlineClass(status) {
  switch (String(status || "").toLowerCase()) {
    case "ok":
      return "badge-ok";
    case "error":
      return "badge-error";
    default:
      return "badge-muted";
  }
}

function cliproxyAuthUsageLabel(status) {
  switch (String(status || "").toLowerCase()) {
    case "ok":
      return t("providerDetail.cliproxyAuthUsageOk");
    case "warning":
      return t("providerDetail.cliproxyAuthUsageWarning");
    case "disabled":
      return t("providerDetail.cliproxyAuthUsageDisabled");
    case "error":
      return t("providerDetail.cliproxyAuthUsageError");
    default:
      return t("providerDetail.cliproxyAuthUsageUnknown");
  }
}

function cliproxyAuthUsageClass(status) {
  switch (String(status || "").toLowerCase()) {
    case "ok":
      return "badge-ok";
    case "warning":
      return "badge-warn";
    case "disabled":
    case "unknown":
      return "badge-muted";
    case "error":
      return "badge-error";
    default:
      return "badge-muted";
  }
}

function cliproxyAuthUsageMetrics(usage) {
  if (!usage) return [];
  if (usage.error) return [{ name: "error", value: usage.error }];
  if (usage.summary?.length) return usage.summary;
  const fallback = usage.note || usage.status_message || "-";
  return [{ name: "status", value: fallback }];
}

function cliproxyAuthUsageMetricName(name) {
  const labels = {
    plan: t("providerDetail.cliproxyAuthUsageMetricPlan"),
    "5h": t("providerDetail.cliproxyAuthUsageMetric5h"),
    "5h_reset": t("providerDetail.cliproxyAuthUsageMetric5hReset"),
    weekly: t("providerDetail.cliproxyAuthUsageMetricWeekly"),
    weekly_reset: t("providerDetail.cliproxyAuthUsageMetricWeeklyReset"),
    auth: t("providerDetail.cliproxyAuthUsageMetricAuth"),
    quota: t("providerDetail.cliproxyAuthUsageMetricQuota"),
    recover_at: t("providerDetail.cliproxyAuthUsageMetricRecoverAt"),
    cooldown: t("providerDetail.cliproxyAuthUsageMetricCooldown"),
    model: t("providerDetail.cliproxyAuthUsageMetricModel"),
    reset_at: t("providerDetail.cliproxyAuthUsageMetricResetAt"),
    reset_after: t("providerDetail.cliproxyAuthUsageMetricResetAfter"),
    remaining: t("providerDetail.cliproxyAuthUsageMetricRemaining"),
    credits: t("providerDetail.cliproxyAuthUsageMetricCredits"),
    credit_balance: t("providerDetail.cliproxyAuthUsageMetricCredits"),
    credits_balance: t("providerDetail.cliproxyAuthUsageMetricCredits"),
  };
  return labels[name] || name;
}

function cliproxyAuthUsageDetails(usage) {
  if (!usage) return "";
  const detail = {};
  if (usage.summary?.length) detail.summary = usage.summary;
  if (usage.data && Object.keys(usage.data).length > 0) detail.data = usage.data;
  if (usage.checked_at) detail.checked_at = usage.checked_at;
  if (usage.cached) detail.cached = true;
  if (usage.note) detail.note = usage.note;
  if (usage.error) detail.error = usage.error;
  return JSON.stringify(detail, null, 2);
}

function cliproxyAuthOnlineDisabled(file) {
  return (
    props.create ||
    !props.name ||
    !isManagedCLIProxyAccess.value ||
    cliproxyAuthVerifying.value[file.filename] ||
    file.validation_status === "invalid"
  );
}

async function refreshCliproxyAuthFiles() {
  if (!isManagedCLIProxyAccess.value) return;
  cliproxyAuthLoading.value = true;
  error.value = "";
  try {
    const result = await fetchCLIProxyAuthFiles();
    cliproxyAuthFiles.value = result.files || [];
    void refreshCliproxyAuthUsageForFiles(cliproxyAuthFiles.value);
  } catch (e) {
    error.value = e.message;
  } finally {
    cliproxyAuthLoading.value = false;
  }
}

async function refreshCliproxyAuthUsageForFiles(files) {
  const requestID = ++cliproxyAuthUsageRequestID.value;
  if (!Array.isArray(files) || files.length === 0) {
    cliproxyAuthUsageResults.value = {};
    cliproxyAuthUsageLoading.value = {};
    return;
  }
  const currentNames = new Set(files.map((file) => file.filename).filter(Boolean));
  const nextResults = {};
  for (const file of files) {
    if (cliproxyAuthUsageResults.value[file.filename]) {
      nextResults[file.filename] = cliproxyAuthUsageResults.value[file.filename];
    }
  }
  cliproxyAuthUsageResults.value = nextResults;
  await Promise.all(
    files.map(async (file) => {
      if (!file?.filename || file.validation_status === "invalid") return;
      cliproxyAuthUsageLoading.value = {
        ...cliproxyAuthUsageLoading.value,
        [file.filename]: true,
      };
      try {
        const result = await fetchCLIProxyAuthFileUsage(file.filename);
        if (!isCurrentCliproxyAuthUsageRequest(requestID, file.filename, currentNames)) return;
        cliproxyAuthUsageResults.value = {
          ...cliproxyAuthUsageResults.value,
          [file.filename]: result,
        };
      } catch (e) {
        if (!isCurrentCliproxyAuthUsageRequest(requestID, file.filename, currentNames)) return;
        cliproxyAuthUsageResults.value = {
          ...cliproxyAuthUsageResults.value,
          [file.filename]: {
            filename: file.filename,
            provider: file.provider,
            status: "error",
            error: e.message,
          },
        };
      } finally {
        if (!isCurrentCliproxyAuthUsageRequest(requestID, file.filename, currentNames)) return;
        cliproxyAuthUsageLoading.value = {
          ...cliproxyAuthUsageLoading.value,
          [file.filename]: false,
        };
      }
    }),
  );
}

function invalidateCliproxyAuthUsageRequests() {
  cliproxyAuthUsageRequestID.value += 1;
}

function isCurrentCliproxyAuthUsageRequest(requestID, filename, expectedNames) {
  if (requestID !== cliproxyAuthUsageRequestID.value) return false;
  if (!expectedNames.has(filename)) return false;
  return cliproxyAuthFiles.value.some((file) => file?.filename === filename);
}

function clearCliproxyAuthDraft() {
  cliproxyAuthContent.value = "";
  cliproxyAuthFilename.value = "";
  if (cliproxyAuthFileInput.value) {
    cliproxyAuthFileInput.value.value = "";
  }
}

function currentCliproxyAuthVerifyModel() {
  if (selectedProbeModel.value) return selectedProbeModel.value;
  return probeableModels.value[0] || "";
}

async function handleCliproxyAuthFileSelect(event) {
  const file = event?.target?.files?.[0];
  if (!file) return;
  cliproxyAuthContent.value = await file.text();
  if (!cliproxyAuthFilename.value) {
    cliproxyAuthFilename.value = file.name;
  }
}

async function importCliproxyAuth() {
  error.value = "";
  message.value = "";
  const content = String(cliproxyAuthContent.value || "").trim();
  if (!content) {
    error.value = t("providerDetail.cliproxyAuthUploadFailed", { error: "content is required" });
    return;
  }
  try {
    const result = await createCLIProxyAuthFile(content, String(cliproxyAuthFilename.value || "").trim());
    message.value = t("providerDetail.cliproxyAuthUploadSuccess", {
      filename: result.file?.filename || "-",
    });
    clearCliproxyAuthDraft();
    await refreshCliproxyAuthFiles();
  } catch (e) {
    error.value = t("providerDetail.cliproxyAuthUploadFailed", { error: e.message });
  }
}

async function verifyCliproxyAuth(file) {
  if (!file?.filename) return;
  error.value = "";
  message.value = "";
  cliproxyAuthVerifying.value = {
    ...cliproxyAuthVerifying.value,
    [file.filename]: true,
  };
  try {
    const result = await verifyCLIProxyAuthFile(
      props.name,
      file.filename,
      currentCliproxyAuthVerifyModel(),
    );
    cliproxyAuthOnlineResults.value = {
      ...cliproxyAuthOnlineResults.value,
      [file.filename]: result,
    };
  } catch (e) {
    cliproxyAuthOnlineResults.value = {
      ...cliproxyAuthOnlineResults.value,
      [file.filename]: {
        status: "error",
        error: e.message,
      },
    };
  } finally {
    cliproxyAuthVerifying.value = {
      ...cliproxyAuthVerifying.value,
      [file.filename]: false,
    };
  }
}

async function deleteCliproxyAuth(file) {
  if (!file?.filename) return;
  if (!window.confirm(t("providerDetail.cliproxyAuthDeleteConfirm", { filename: file.filename }))) return;
  error.value = "";
  message.value = "";
  cliproxyAuthDeleting.value = {
    ...cliproxyAuthDeleting.value,
    [file.filename]: true,
  };
  try {
    await deleteCLIProxyAuthFile(file.filename);
    const nextResults = { ...cliproxyAuthOnlineResults.value };
    delete nextResults[file.filename];
    cliproxyAuthOnlineResults.value = nextResults;
    const nextUsageResults = { ...cliproxyAuthUsageResults.value };
    delete nextUsageResults[file.filename];
    cliproxyAuthUsageResults.value = nextUsageResults;
    message.value = t("providerDetail.cliproxyAuthDeleteSuccess", { filename: file.filename });
    await refreshCliproxyAuthFiles();
  } catch (e) {
    error.value = t("providerDetail.cliproxyAuthDeleteFailed", { error: e.message });
  } finally {
    cliproxyAuthDeleting.value = {
      ...cliproxyAuthDeleting.value,
      [file.filename]: false,
    };
  }
}

function pruneProviderReferences(nextConfig, targetProvider) {
  if (!nextConfig?.route || typeof nextConfig.route !== "object") return;

  for (const [prefix, route] of Object.entries(nextConfig.route)) {
    if (!route || typeof route !== "object") continue;

    const nextExactModels = {};
    for (const [modelName, modelConfig] of Object.entries(
      route.exact_models || {},
    )) {
      if (!modelConfig || typeof modelConfig !== "object") continue;
      const upstreams = (modelConfig.upstreams || []).filter(
        (upstream) => upstream?.provider !== targetProvider,
      );
      if (upstreams.length === 0) continue;
      nextExactModels[modelName] = { ...modelConfig, upstreams };
    }

    const nextWildcardModels = {};
    for (const [pattern, modelConfig] of Object.entries(
      route.wildcard_models || {},
    )) {
      if (!modelConfig || typeof modelConfig !== "object") continue;
      const providers = (modelConfig.providers || []).filter(
        (provider) => provider !== targetProvider,
      );
      if (providers.length === 0) continue;
      nextWildcardModels[pattern] = { ...modelConfig, providers };
    }

    if (
      Object.keys(nextExactModels).length === 0 &&
      Object.keys(nextWildcardModels).length === 0
    ) {
      delete nextConfig.route[prefix];
      continue;
    }

    route.exact_models = nextExactModels;
    route.wildcard_models = nextWildcardModels;
  }
}

async function load() {
  loading.value = true;
  suppressDirty.value = true;
  error.value = "";
  healthResult.value = null;
  configFileChanged.value = false;
  protocolProbeResult.value = null;
  try {
    const [cfg, source, formMeta] = await Promise.all([
      fetchConfig(),
      fetchConfigSource(),
      fetchProviderFormMeta(),
    ]);
    configDoc.value = cfg;
    configSource.value = source;
    providerFormMeta.value = formMeta;
    showAPIKey.value = false;
    apiKeyTouched.value = false;
    invalidateCliproxyAuthUsageRequests();
    cliproxyAuthFiles.value = [];
    cliproxyAuthUsageResults.value = {};
    cliproxyAuthUsageLoading.value = {};
    clearCliproxyAuthDraft();

    if (props.create) {
      providerName.value = "";
      detail.value = null;
      selectedProbeModel.value = "";
      selectedPresetId.value = "";
      providerConfig.value = createEmptyProviderConfig();
      const defaultPresetID = providerPresets.value[0]?.id || "";
      if (defaultPresetID) {
        applyPresetByID(defaultPresetID);
      }
    } else {
      providerName.value = props.name;
      const provider = cfg.provider?.[props.name];
      if (!provider) {
        throw new Error(
          t("providerDetail.providerConfigMissing", { name: props.name }),
        );
      }
      providerConfig.value = {
        ...createEmptyProviderConfig(),
        ...cloneData(provider),
        family: provider.family || provider.protocol || "",
        service_protocols: [...(provider.service_protocols || [])],
        models: [...(provider.models || [])],
        headers: cloneData(provider.headers || {}),
      };
      selectedPresetId.value = inferPresetID(providerConfig.value);
      selectedAuthSource.value = inferAuthSource(providerConfig.value);
      syncSelectedServiceTemplate();
      detail.value = await fetchProviderDetail(props.name);
      if (!selectedProbeModel.value && discoveredModelIds.value.length > 0) {
        selectedProbeModel.value = discoveredModelIds.value[0];
      }
    }

    dirty.value = false;
  } catch (e) {
    error.value = e.message;
  } finally {
    suppressDirty.value = false;
    loading.value = false;
  }
}

function discard() {
  if (!confirm(t("config.confirmDiscard"))) return;
  load();
}

async function apply() {
  saving.value = true;
  message.value = "";
  error.value = "";
  try {
    if (!configSource.value?.source_type?.file) {
      error.value = t("config.savingDisabled");
      return;
    }

    const name = providerName.value.trim();
    if (!name) {
      error.value = t("providerDetail.nameRequired");
      return;
    }
    if (!providerFamily(providerConfig.value)) {
      error.value = t("providerDetail.familyRequired");
      return;
    }
    const family = providerFamily(providerConfig.value);
    if (!["copilot"].includes(family) && !providerConfig.value.url?.trim()) {
      error.value = t("providerDetail.urlRequired");
      return;
    }
    const selectedAuthMode = authMode.value;
    if (
      selectedAuthMode === "command" &&
      !String(providerConfig.value.api_key_command || "").trim()
    ) {
      error.value = t("providerDetail.apiKeyCommandRequired");
      return;
    }
    const backend =
      family === "openai" ? providerBackend(providerConfig.value) : "";
    if (backend === "cliproxy") {
      if (!String(providerConfig.value.backend_provider || "").trim()) {
        error.value = t("providerDetail.backendProviderRequired");
        return;
      }
      if ((providerConfig.value.service_protocols || []).length === 0) {
        error.value = t("providerDetail.backendServiceProtocolsRequired");
        return;
      }
    }

    const nextConfig = cloneData(configDoc.value);
    nextConfig.provider = nextConfig.provider || {};

    if (props.create && nextConfig.provider[name]) {
      error.value = t("providerDetail.providerExists", { name });
      return;
    }

    const nextProviderConfig = cloneData(providerConfig.value);
    nextProviderConfig.family = providerFamily(nextProviderConfig);
    nextProviderConfig.backend =
      nextProviderConfig.family === "openai"
        ? providerBackend(nextProviderConfig)
        : "";
    nextProviderConfig.backend_provider = normalizeLowerText(
      nextProviderConfig.backend_provider,
    );
    nextProviderConfig.service_protocols = normalizeServiceProtocols(
      nextProviderConfig.service_protocols,
    );
    if (!nextProviderConfig.backend) {
      delete nextProviderConfig.backend;
      delete nextProviderConfig.backend_provider;
    }
    if (nextProviderConfig.service_protocols.length === 0) {
      delete nextProviderConfig.service_protocols;
    }
    delete nextProviderConfig.protocol;
    applyProviderAuthSource(nextProviderConfig, selectedAuthMode);
    nextConfig.provider[name] = nextProviderConfig;

    const cleaned = cleanConfig(nextConfig);
    const result = await validateConfig(cleaned);
    if (!result.valid) {
      error.value = t("config.validationFailed", { error: result.error });
      return;
    }

    await saveConfig(cleaned);
    const restart = await restartGateway();
    if (restart.status !== "ok") {
      error.value = t("config.savedButRestartFailed", {
        error: restart.error || "unknown error",
      });
      return;
    }

    const alive = await pollUntilAlive(
      fetchStatus,
      bindPollState(waitingAlive, waitingElapsed),
    );
    if (!alive) {
      error.value = t("config.serviceTimeout");
      return;
    }

    if (props.create) {
      await router.replace(`/providers/${encodeURIComponent(name)}`);
    } else {
      await load();
    }

    message.value = t("providerDetail.savedMsg", { name });
  } catch (e) {
    if (e.message?.includes("config file changed externally")) {
      configFileChanged.value = true;
      error.value = t("config.externalChangeError");
    } else {
      error.value = e.message;
    }
  } finally {
    saving.value = false;
  }
}

async function deleteProvider() {
  if (
    !confirm(
      t("providerDetail.confirmDeleteProvider", { name: providerName.value }),
    )
  )
    return;

  saving.value = true;
  message.value = "";
  error.value = "";
  try {
    if (!configSource.value?.source_type?.file) {
      error.value = t("config.savingDisabled");
      return;
    }

    const nextConfig = cloneData(configDoc.value);
    delete nextConfig.provider?.[providerName.value];
    pruneProviderReferences(nextConfig, providerName.value);

    const cleaned = cleanConfig(nextConfig);
    const result = await validateConfig(cleaned);
    if (!result.valid) {
      error.value = t("config.validationFailed", { error: result.error });
      return;
    }

    await saveConfig(cleaned);
    const restart = await restartGateway();
    if (restart.status !== "ok") {
      error.value = t("config.savedButRestartFailed", {
        error: restart.error || "unknown error",
      });
      return;
    }

    const alive = await pollUntilAlive(
      fetchStatus,
      bindPollState(waitingAlive, waitingElapsed),
    );
    if (!alive) {
      error.value = t("config.serviceTimeout");
      return;
    }

    await router.push("/providers");
  } catch (e) {
    if (e.message?.includes("config file changed externally")) {
      configFileChanged.value = true;
      error.value = t("config.externalChangeError");
    } else {
      error.value = e.message;
    }
  } finally {
    saving.value = false;
  }
}

async function runHealthCheck() {
  checking.value = true;
  healthResult.value = null;
  try {
    healthResult.value = await healthCheck(props.name);
  } catch (e) {
    healthResult.value = { status: "error", error: e.message };
  } finally {
    checking.value = false;
  }
}

async function runProtocolDetect() {
  if (!props.name) return;
  detectingProtocols.value = true;
  error.value = "";
  try {
    await detectProviderProtocols(props.name);
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    detectingProtocols.value = false;
  }
}

async function runExactProtocolProbe() {
  if (!props.name || !selectedProbeModel.value) return;
  exactProbing.value = true;
  error.value = "";
  protocolProbeResult.value = null;
  try {
    protocolProbeResult.value = await probeProviderModelProtocol(
      props.name,
      selectedProbeModel.value,
      selectedProbeProtocol.value,
    );
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    exactProbing.value = false;
  }
}

async function suppressProvider() {
  try {
    await setProviderSuppress(props.name, true);
    await load();
  } catch (e) {
    error.value = e.message;
  }
}

async function unsuppressProvider() {
  try {
    await setProviderSuppress(props.name, false);
    await load();
  } catch (e) {
    error.value = e.message;
  }
}
</script>

<style scoped>
.section-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 14px;
}

.section-desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--c-text-3);
  max-width: 760px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.runtime-actions {
  margin-top: -8px;
}

.health-result {
  font-size: 13px;
}

.provider-form {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.form-panel {
  border-top: 1px solid var(--c-border);
  padding-top: 18px;
}

.form-panel:first-of-type {
  border-top: none;
  padding-top: 0;
}

.form-panel-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
}

.form-panel-head h4 {
  margin: 0;
  font-size: 15px;
}

.panel-badges {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.form-grid {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: 10px 14px;
  align-items: start;
}

.compact-grid {
  grid-template-columns: 150px 1fr;
}

.form-grid > label {
  padding-top: 7px;
  font-size: 12px;
  color: var(--c-text-2);
  font-family: var(--font-mono);
}

.form-grid-full {
  grid-column: 1 / -1;
}

.field-stack {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.auth-source-details {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  padding-top: 4px;
}

.auth-detail-label {
  font-size: 12px;
  color: var(--c-text-2);
  font-family: var(--font-mono);
}

.auth-command-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.field-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.protocol-chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.interface-preview {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  background: var(--c-bg-soft);
  border: 1px solid var(--c-border);
  border-radius: 8px;
}

.interface-preview-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-2);
}

.custom-interface-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--c-border);
  border-radius: 8px;
}

.cliproxy-auth-panel {
  grid-column: 1 / -1;
  margin-top: 6px;
  min-width: 0;
}

.cliproxy-auth-import {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.cliproxy-auth-file-row {
  display: grid;
  grid-template-columns: minmax(180px, 0.7fr) minmax(180px, 1fr);
  gap: 10px;
}

.cliproxy-auth-textarea {
  min-height: 120px;
  resize: vertical;
  white-space: pre;
  font-family: var(--font-mono);
}

.auth-file-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

.auth-files-cards {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.auth-file-card {
  display: grid;
  grid-template-columns: minmax(0, 1.3fr) minmax(180px, 0.9fr) minmax(120px, auto);
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: 8px;
  background: var(--c-surface);
}

.auth-file-card-main,
.auth-file-status {
  min-width: 0;
}

.auth-file-name {
  display: block;
  overflow-wrap: anywhere;
}

.auth-file-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 10px;
  margin-top: 6px;
  color: var(--c-text-3);
  font-size: 12px;
}

.auth-file-status {
  display: grid;
  gap: 6px;
}

.auth-file-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}

.auth-file-action {
  white-space: nowrap;
}

.validation-message {
  display: block;
  margin-top: 4px;
  color: var(--c-text-3);
  line-height: 1.35;
}

.usage-message {
  position: relative;
  width: fit-content;
  max-width: 100%;
  cursor: help;
}

.usage-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 0;
}

.usage-metric {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 2px 6px;
  border: 1px solid var(--c-border);
  border-radius: 6px;
  background: var(--c-bg-soft);
  color: var(--c-text-2);
  font-size: 12px;
  line-height: 1.35;
}

.usage-metric-name {
  color: var(--c-text-3);
}

.usage-metric-value {
  overflow-wrap: anywhere;
  color: var(--c-text);
}

.usage-metric-muted {
  color: var(--c-text-3);
}

.usage-tooltip {
  position: absolute;
  z-index: 20;
  left: 0;
  top: calc(100% + 6px);
  display: none;
  width: min(520px, 70vw);
  max-height: 320px;
  overflow: auto;
  margin: 0;
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: 8px;
  background: var(--c-bg);
  color: var(--c-text);
  box-shadow: var(--shadow-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.45;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.usage-message:hover .usage-tooltip,
.usage-message:focus .usage-tooltip,
.usage-message:focus-within .usage-tooltip {
  display: block;
}

.custom-interface-head {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.section-note {
  font-size: 13px;
  color: var(--c-text-3);
}

.runtime-panel summary {
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
}

.summary-count {
  margin-left: 8px;
  color: var(--c-text-3);
  font-size: 12px;
  font-weight: normal;
}

.advanced-desc {
  margin-bottom: 12px;
}

.runtime-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.compact-top {
  margin-bottom: 8px;
}

.runtime-panel {
  padding-top: 14px;
}

.runtime-panel summary {
  margin-bottom: 12px;
}

.runtime-tables {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 14px;
}

.probe-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 220px) auto;
  gap: 10px;
  align-items: center;
  margin-top: 12px;
}

.probe-results-table {
  margin-top: 12px;
}

.req {
  color: var(--c-danger);
}

.hint {
  color: var(--c-text-3);
  font-size: 11px;
  font-weight: normal;
}

.secret-field {
  display: flex;
  gap: 8px;
  align-items: center;
}

.secret-field .form-input {
  flex: 1;
}

.form-hint-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 34px;
}

.service-protocols-editor {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.service-protocols-hint {
  margin: 0;
}

.models-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.models-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.models-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.models-hint {
  display: block;
  margin: 0;
}

.form-checkbox {
  width: 16px;
  height: 16px;
  margin-top: 1px;
}

.badge-none {
  background: var(--c-border-light);
  color: var(--c-text-3);
}

.badge-muted {
  background: var(--c-bg-soft);
  color: var(--c-text-2);
}

@media (max-width: 768px) {
  .section-top,
  .form-panel-head {
    flex-direction: column;
  }

  .form-grid,
  .probe-grid,
  .cliproxy-auth-file-row,
  .auth-file-card,
  .auth-command-grid {
    grid-template-columns: 1fr;
  }

  .form-grid > label {
    padding-top: 0;
  }

  .secret-field,
  .models-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
