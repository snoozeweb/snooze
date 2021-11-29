<template>
  <div>
    <CRow>
      <CCol col=3 md=2>
        <label :id="'title_' + metadata.display_name" v-c-tooltip="{content: this.metadata.description, placement: 'right'}">{{ metadata.display_name }}</label><label v-if="metadata.required">*</label>
      </CCol>
      <CCol col=9 md=10>
        <component
          v-model="datavalue"
          :id="'component_'+metadata.display_name"
          :is="component"
          :data="data"
          :options="metadata.options"
          :disabled="metadata.disabled"
          :required="metadata.required"
          :colorize="metadata.colorize"
          :import_keys="metadata.import"
          :placeholder="metadata.placeholder"
          :default_value="metadata.default_value"
          :endpoint="metadata.endpoint"
          :primary="metadata.primary"
          :form="metadata.form"
        />
      </CCol>
    </CRow>

  </div>
</template>

<script>
import { defineAsyncComponent, shallowRef } from 'vue'
// @group Forms
// Base class for all form inputs
export default {
  emits: ['update:modelValue'],
  props: {
    modelValue: {},
    metadata: {type: Object, default: () => {}},
    data: {type: Object},
  },
  data() {
    return {
      datavalue: (this.modelValue != undefined) ? this.modelValue : (this.metadata ? this.metadata.default : {}),
      component: shallowRef(defineAsyncComponent(() => import(`./${this.metadata.component}.vue`))),
      popover: {content: this.metadata ? (this.metadata.description || '') : '', trigger: ['hover', 'focus'], placement: 'right'}
    }
  },
  watch: {
    datavalue () {
      // Return the value of the input form
      this.$emit('update:modelValue', this.datavalue)
    }
  },
}

</script>
