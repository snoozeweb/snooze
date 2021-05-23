<template>
  <div class="animated fadeIn">
    <Card
      endpoint="settings"
      :tabs="tabs"
      :form="form"
      :onSubmit="submit"
      ref="card"
    >
    </Card>
  </div>
</template>

<script>

import { API } from '@/api'
import Card from '@/components/Card.vue'
import { tabs, form } from '@/objects/Settings.yaml'

export default {
  components: {
    Card,
  },
  mounted () {
  },
  methods: {
    submit(data) {
      this.$refs.card.submit(data, this[this.$refs.card.current_tab.callback])
    },
    ldap_reload() {
      console.log("Reloading LDAP")
      API
        .post(`/login/reload`, {'method': 'ldap'})
        .then(response => {
          console.log(response)
          if (!response.data) {
            if(response.response.data.description) {
              this.$refs.card.makeToast(response.response.data.description, 'danger', 'Reload error')
            } else {
              this.$refs.card.makeToast('Could not reload LDAP', 'danger', 'An error occurred')
            }
          }
        })
        .catch(error => console.log(error))
    },
    general_reload() {
      console.log("Reloading General")
      API
        .post(`/login/reload`, {'method': 'local'})
        .then(response => {
          console.log(response)
          if (!response.data) {
            if(response.response.data.description) {
              this.$refs.card.makeToast(response.response.data.description, 'danger', 'Reload error')
            } else {
              this.$refs.card.makeToast('Could not reload General', 'danger', 'An error occurred')
            }
          }
        })
        .catch(error => console.log(error))
    }
  },
  data () {
    return {
      tabs: tabs,
      form: form
    }
  },
}
</script>
