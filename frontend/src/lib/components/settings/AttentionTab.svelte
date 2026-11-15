<script lang="ts">
  import { Label } from '$lib/components/ui/label'
  import Switch from '$lib/components/ui/switch/Switch.svelte'
  import { _ } from '$lib/i18n'

  interface Props {
    attentionEnabled: boolean
    attentionTypeVerificationCode: boolean
    attentionTypeFlightChanged: boolean
    attentionTypePaymentFailed: boolean
    attentionTypeDeliveryProblem: boolean
    onAttentionEnabledChange: (value: boolean) => void
    onAttentionTypeChange: (type: string, value: boolean) => void
  }

  let {
    attentionEnabled = $bindable(),
    attentionTypeVerificationCode = $bindable(),
    attentionTypeFlightChanged = $bindable(),
    attentionTypePaymentFailed = $bindable(),
    attentionTypeDeliveryProblem = $bindable(),
    onAttentionEnabledChange,
    onAttentionTypeChange,
  }: Props = $props()

  function handleTypeChange(type: string, value: boolean) {
    onAttentionTypeChange?.(type, value)
  }
</script>

<div class="space-y-6">
  <!-- Master toggle -->
  <div class="space-y-2">
    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="attention-enabled">{$_('settingsAttention.enabled')}</Label>
        <p class="text-xs text-muted-foreground">
          {$_('settingsAttention.enabledHelp')}
        </p>
      </div>
      <Switch
        id="attention-enabled"
        bind:checked={attentionEnabled}
        onCheckedChange={(v) => onAttentionEnabledChange?.(v)}
      />
    </div>
  </div>

  <!-- Divider -->
  <div class="border-t border-border"></div>

  <!-- Per-type toggles -->
  <div class="space-y-4">
    <p class="text-sm font-medium">{$_('settingsAttention.typesHeading')}</p>

    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="attention-type-code">{$_('attention.typeVerificationCode')}</Label>
        <p class="text-xs text-muted-foreground">{$_('settingsAttention.typeVerificationCodeHelp')}</p>
      </div>
      <Switch
        id="attention-type-code"
        bind:checked={attentionTypeVerificationCode}
        disabled={!attentionEnabled}
        onCheckedChange={(v) => handleTypeChange('verification_code', v)}
      />
    </div>

    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="attention-type-flight">{$_('attention.typeFlightChanged')}</Label>
        <p class="text-xs text-muted-foreground">{$_('settingsAttention.typeFlightChangedHelp')}</p>
      </div>
      <Switch
        id="attention-type-flight"
        bind:checked={attentionTypeFlightChanged}
        disabled={!attentionEnabled}
        onCheckedChange={(v) => handleTypeChange('flight_changed', v)}
      />
    </div>

    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="attention-type-payment">{$_('attention.typePaymentFailed')}</Label>
        <p class="text-xs text-muted-foreground">{$_('settingsAttention.typePaymentFailedHelp')}</p>
      </div>
      <Switch
        id="attention-type-payment"
        bind:checked={attentionTypePaymentFailed}
        disabled={!attentionEnabled}
        onCheckedChange={(v) => handleTypeChange('payment_failed', v)}
      />
    </div>

    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="attention-type-delivery">{$_('attention.typeDeliveryProblem')}</Label>
        <p class="text-xs text-muted-foreground">{$_('settingsAttention.typeDeliveryProblemHelp')}</p>
      </div>
      <Switch
        id="attention-type-delivery"
        bind:checked={attentionTypeDeliveryProblem}
        disabled={!attentionEnabled}
        onCheckedChange={(v) => handleTypeChange('delivery_problem', v)}
      />
    </div>
  </div>
</div>
