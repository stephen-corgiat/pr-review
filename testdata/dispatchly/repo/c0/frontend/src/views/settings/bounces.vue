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
