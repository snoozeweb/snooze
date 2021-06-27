<template>
  <div>
    <b-form-tags
      id="tags-component-select"
      v-model="datavalue"
      size="lg"
      class="mb-2"
      add-on-change
      no-outer-focus
    >
      <template v-slot="{ tags, inputAttrs, inputHandlers, disabled, removeTag }">
        <ul v-if="tags.length > 0" class="list-inline d-inline-block mb-2">
          <li v-for="tag in tags" :key="tag" class="list-inline-item">
            <b-form-tag
              @remove="removeTag(tag)"
              :title="tag"
              :disabled="disabled"
              :variant="get_color(colorize?tag:'')"
            >{{ tag }}</b-form-tag>
          </li>
        </ul>
        <b-form-select
          v-bind="inputAttrs"
          v-on="inputHandlers"
          :disabled="disabled || availableOptions.length === 0"
          :options="availableOptions"
        >
          <template #first>
            <!-- This is required to prevent bugs with Safari -->
            <option disabled v-if="!default_value && default_value != ''" value="">Choose an option...</option>
          </template>
        </b-form-select>
      </template>
    </b-form-tags>
  </div>
</template>

<script>

import Base from './Base.vue'
import { get_color } from '@/utils/colors'

// Create a selector form
export default {
  extends: Base,
  props: {
    value: {
      type: Array,
    },
    // Object containing the `{value: display_name}` of the
    // options of the selector
    options: {
      type: Array,
    },
    colorize: {
      type: Boolean,
    },
    default_value: {
      type: Array,
    },
  },
  data() {
    return {
      get_color: get_color,
      datavalue: [undefined, '', [], {}].includes(this.value) ? (this.default_value == undefined ? [] : this.default_value) : this.value,
    }
  },
  computed: {
    availableOptions() {
      return this.options.filter(opt => this.datavalue.indexOf(opt) === -1)
    }
  },
  watch: {
    datavalue () {
      this.$emit('input', this.datavalue)
    }
  },
}

</script>
