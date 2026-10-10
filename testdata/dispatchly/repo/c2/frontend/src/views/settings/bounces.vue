<template>
  <div class="bounces">
    <b-field :label="$t('settings.bounces.enableWebhooks')">
      <b-switch v-model="data['bounce.webhooks_enabled']" />
    </b-field>
    <div v-if="data['bounce.webhooks_enabled']">
      <b-field :label="$t('settings.bounces.enableSendgrid')">
        <b-switch v-model="data['bounce.sendgrid'].enabled" />
      </b-field>
      <b-field :label="$t('settings.bounces.enablePostmark')">
        <b-switch v-model="data['bounce.postmark'].enabled" />
      </b-field>
      <b-field :label="$t('settings.bounces.enableBeacon')">
        <b-switch v-model="data['bounce.beacon'].enabled" />
      </b-field>
      <div v-if="data['bounce.beacon'].enabled">
        <b-field :label="$t('settings.bounces.beaconSecret')" :message="$t('settings.bounces.beaconSecretHelp')">
          <b-input v-model="data['bounce.beacon'].shared_secret" type="password" />
        </b-field>
        <b-field :label="$t('settings.bounces.beaconSecretHeader')">
          <b-input v-model="data['bounce.beacon'].shared_secret_header" />
        </b-field>
      </div>
    </div>
  </div>
</template>

<script>
export default {
  props: { form: { type: Object, default: () => ({}) } },
  data() {
    return { data: this.form };
  },
};
</script>
