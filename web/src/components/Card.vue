<template>
  <div>
  <CForm @submit.prevent="checkForm" novalidate>
    <CCard v-if="current_tab" no-body ref="main">
      <CCardHeader header-tag="nav" class="p-2">
        <CNav variant="pills" role="tablist" card v-model="tab_index" class='m-0'>
          <CNavItem
            v-for="tab in tabs"
            v-bind:key="tab.key"
            v-on:click="changeTab(tab)"
          >
            <CNavLink href="javascript:void(0);" :active="tab.key == current_tab.key">{{ tab.name }}</CNavLink>
          </CNavItem>
        </CNav>
      </CCardHeader>
      <CCardBody class="p-2">
        <Form v-model="form_data" :metadata="form[current_tab.key]" :key="form_key" ref='form'/>
      </CCardBody>
      <CCardFooter class="p-2">
        <CButton type="submit" :color="save_variant" :disabled="save_disabled">Save {{ current_tab.name }}</CButton>
      </CCardFooter>
    </CCard>
  </CForm>
  </div>
</template>

<script>
import { API } from '@/api'
import dig from 'object-dig'
import Form from '@/components/Form.vue'
import { get_data } from '@/utils/api'

// Create a card fed by an API endpoint.
export default {
  name: 'Card',
  components: {
    Form
  },
  props: {
    // The tabs name and their associated search
    tabs_prop: {
      type: Array,
      default: () => { return [] },
    },
    // The API path to query
    endpoint_prop: {
      type: String,
      required: true,
    },
    form_prop: {
      type: Object,
      default: () => { return {} },
    },
    loaded_callback: {
      type: Function,
    },
    onSubmit: {
      type: Function,
    }
  },
  mounted () {
    this.save_enable()
    this.schema = JSON.parse(localStorage.getItem(this.endpoint+'_json') || '{}')
    get_data(`schema/${this.endpoint}`, null, {}, this.load_table)
  },
  data () {
    return {
      form: this.form_prop,
      tabs: this.tabs_prop,
      form_data: {},
      form_key: 0,
      endpoint: this.endpoint_prop,
      current_endpoint: this.endpoint_prop,
			current_tab: {},
      save_disabled: null,
      save_variant: null,
      submitForm: this.onSubmit || this.submit,
      schema: {},
      loaded: false,
    }
  },
  computed: {
  },
  methods: {
    load_table(response) {
      if (response.data) {
        if (response.data.count > 0) {
          this.schema = response.data
          localStorage.setItem(this.endpoint+'_json', JSON.stringify(response.data))
        }
        var data = this.schema
        this.form = dig(data, 'form')
        this.tabs = dig(data, 'tabs')
        this.endpoint = dig(data, 'endpoint') || this.endpoint
        this.current_tab = this.tabs[0]
        if (this.loaded_callback) {
          this.loaded_callback()
        }
        this.reload()
      }
    },
    reload() {
      var tab = this.tabs[0]
      if (this.$route.query.tab !== undefined) {
        var find_tab = this.tabs.find(el => el.key == this.$route.query.tab)
        if (tab) {
          tab = find_tab
        }
      }
      if (tab) {
        this.changeTab(tab, false)
      }
    },
    checkForm() {
      if (this.$el.getElementsByClassName('form-control is-invalid').length > 0) {
        this.$root.text_alert('Form is invalid', 'danger')
        return
      }
      this.submitForm(this.form_data)
    },
    get_config_data() {
      API
        .get(`/${this.current_endpoint}`)
        .then(response => {
          if (response.data) {
            this.form_data = response.data.data[0] || {}
          } else {
            if(response.response.data.description) {
              this.$root.text_alert(response.response.data.description, 'danger')
            } else {
              this.$root.text_alert('Could not display the content', 'danger')
            }
          }
          this.forceRerender()
        })
        .catch(error => console.log(error))
		},
    changeTab(new_tab, update_history = true) {
      this.current_tab = new_tab
      if (this.current_tab.endpoint) {
        this.current_endpoint = this.current_tab.endpoint
      } else {
        this.current_endpoint = this.endpoint + '/' + this.current_tab.key
      }
      this.form_data = {}
      this.get_config_data()
      if (update_history) {
        this.add_history()
      }
    },
    save_enable() {
      this.save_disabled = false
      this.save_variant = 'success'
    },
    save_disable() {
      this.save_disabled = true
      this.save_variant = 'secondary'
    },
    submit(data, callback = null) {
      console.log(`PUT /${this.current_endpoint}`)
      API
        .put(`/${this.current_endpoint}`, data)
        .then(response => {
          console.log(response)
          if (response.data) {
            if (callback) {
              callback(response.data)
            }
            this.$root.text_alert(`Saved ${this.current_tab.name}`, 'success', 'Save successful')
          } else {
            if(response.response.data.description) {
              this.$root.text_alert(response.response.data.description, 'danger', 'Save error')
            } else {
            this.$root.text_alert(`Failed to save ${this.current_tab.name}`, 'danger', 'Save error')
            }
          }
        })
        .catch(error => console.log(error))
    },
    forceRerender() {
      this.form_key += 1
      this.loaded = true
    },
    add_history() {
      const query = { tab: this.current_tab.key }
      if (this.$route.query.tab != query.tab) {
        this.$router.push({ path: this.$router.currentRoute.value.path, query: query })
      }
    },
  },
  watch: {
    form_data () {
      this.$emit('input', this.form_data)
    },
    $route() {
      if (this.loaded && this.$route.path == `/${this.endpoint_prop}`) {
        this.$nextTick(this.reload);
      }
    }
  },
}
</script>

<style>

.fix-nav {
  height: 100%;
}

</style>
