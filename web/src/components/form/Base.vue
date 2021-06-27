<template>
  <div>
    <b-row>
      <b-col cols=3 md=2>
        <label :id="'title_' + metadata.display_name" >{{ metadata.display_name }}</label><label v-if="metadata.required">*</label>
        <b-popover
          :target="'title_' + metadata.display_name"
          :content="metadata.description"
          triggers="hover focus"
          placement="right"
        ></b-popover>
      </b-col>
      <b-col cols=9 md=10>
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
        />
      </b-col>
    </b-row>

  </div>
</template>

<script>
// @group Forms
// Base class for all form inputs
export default {
  props: {
    value: {},
    metadata: {type: Object, default: () => {}},
    data: {type: Object},
  },
  data() {
    return {
      datavalue: (this.value != undefined) ? this.value : (this.metadata ? this.metadata.default : {})
    }
  },
  computed: {
    component () {
      return () => import(`./${this.metadata.component}.vue`)
    },
  },
  watch: {
    datavalue () {
      // Return the value of the input form
      this.$emit('input', this.datavalue)
    }
  },
}

</script>
