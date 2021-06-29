<template>
  <div>
    <b-form-tags
      id="tags-component-select"
      v-model="dataval"
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
              :no-remove="is_tag_locked(tag)"
            >{{ tag }} <i v-if="is_tag_locked(tag)" class="la la-lock"/></b-form-tag>
          </li>
        </ul>
        <b-form-select
					v-bind="inputAttrs"
          v-on="inputHandlers"
          :disabled="disabled || availableOptions.length === 0"
				>
          <template #first>
            <!-- This is required to prevent bugs with Safari -->
            <option disabled value="">Choose an option...</option>
          </template>
          <option v-for="item in availableOptions" :key="item[primary] || item" :value="item[primary] || item">
            {{ item['name'] || item }}
          </option>
        </b-form-select>
      </template>
    </b-form-tags>
  </div>
</template>

<script>
import { API } from '@/api'
import Base from './Base.vue'
import { get_color } from '@/utils/colors'

export default {
  extends: Base,
  props: {
    value: {},
    // Endpoint of the API to query and
    // fetch the objects
    endpoint: {
      type: String,
      required: true,
    },
    primary: {
      type: String,
    },
    data: {
      type: Object,
    },
    import_keys: {
      type: Array,
      default: () => [],
    },
    colorize: {
      type: Boolean,
    },
  },
  data() {
    return {
      get_color: get_color,
      datavalue: this.value || [],
      items: [],
      empty_message: "Please select a value",
    }
  },
  watch: {
    datavalue: {
      handler: function () {
        this.$emit('input', this.datavalue)
      },
      immediate: true
    },
  },
  mounted () {
    this.reload_items()
  },
  computed: {
    availableOptions() {
      return this.items.filter(opt => this.dataval.indexOf(opt[this.primary] || opt) === -1)
    },
    dataval: {
      get: function () {
        return this.datavalue.concat(this.data[this.import_keys[0]] || [])
      },
      set: function(newvalue) {
        this.datavalue = newvalue.filter((el) => !(this.data[this.import_keys[0]] || []).includes(el));
      }
    },
    is_tag_locked() {
      return function (tag) {
        return (this.data[this.import_keys[0]] || []).includes(tag)
      }
    },
  },
  methods: {
    reload_items () {
      console.log(`GET /${this.endpoint}`)
      API
        .get(`/${this.endpoint}`)
        .then(response => {
          this.items = response.data.data
        })
    },
  },
}
</script>
