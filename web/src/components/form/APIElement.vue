<template>
  <div>
    <b-form-select v-model="selected" aria-describedby="feedback" :required="required" :state="checkField" @input="selection_changed">
      <option disabled value="">{{ this.empty_message }}</option>
      <option v-for="item in this.items" :key="item[primary]" :value="item[primary]">
        {{ item['name'] }}
      </option>
    </b-form-select>
    <b-form-invalid-feedback id="feedback" :state="checkField">
      Field is required
    </b-form-invalid-feedback>
    <Form v-if="selection && selection.form" v-model="subcontent" :metadata="selection.form" class="pt-2"/>
  </div>
</template>

<script>
import { API } from '@/api'
import Base from './Base.vue'
import Form from '@/components/Form.vue'

export default {
  extends: Base,
  components: {
    Form
  },
  props: {
    value: {
      type: [Object, String],
      default: function () {
        return {'selected': '', 'subcontent': {}}
      }
    },
    // Endpoint of the API to query and
    // fetch the objects
    endpoint: {
      type: String,
      required: true,
    },
    primary: {
      type: String,
      required: true,
    },
    subkey: {
      type: String,
    },
    required: {
      type: Boolean,
      default: () => false
    },
  },
  data() {
    return {
      selected: (this.value['selected'] == undefined) ? this.value : this.value['selected'],
      subcontent: this.value['subcontent'],
      items: [],
      empty_message: "Please select a value",
    }
  },
  watch: {
    selected: {
      handler: function () {
        if (this.subcontent && Object.keys(this.subcontent).length > 0 && this.subcontent.constructor === Object) {
          this.$emit('input', {'selected': this.selected, 'subcontent': this.subcontent})
        } else {
          this.$emit('input', this.selected)
        }
      },
      immediate: true
    },
    subcontent: {
      handler: function () {
        this.$emit('input', {'selected': this.selected, 'subcontent': this.subcontent})
      },
      immediate: true
    },
  },
  mounted () {
    this.reload_items()
  },
  methods: {
    reload_items() {
      console.log(`GET /${this.endpoint}`)
      API
        .get(`/${this.endpoint}`)
        .then(response => {
          console.log(response)
          if (response.data) {
            this.items = response.data.data
            if (this.subkey && this.items[this.subkey]) {
              this.items = this.items[this.subkey]
            }
          }
        })
    },
    selection_changed() {
      this.subcontent = {}
    }
  },
  computed: {
    selection() {
      return this.items.find(opt => opt[this.primary] == this.selected)
    },
    checkField () {
      if (!this.required) {
        return null
      } else {
        return this.required && this.selected != ''
      }
    }
  },
}
</script>
