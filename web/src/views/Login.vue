<template>
  <div class="app d-flex flex-row align-items-center" style="min-height:100vh">
    <div class="container pb-5">
      <b-row class="justify-content-center px-5 pb-5">
        <h1><img src="img/logo.png" :height="160"></h1>
      </b-row>
      <b-row class="justify-content-center">
        <b-col md="8">
          <b-card no-body>
            <b-tabs card pills v-model="tabIndex">
              <b-tab v-for="backend in this.auth_backends" :key="backend['endpoint']" :title="backend['name']"><b-card-text><basic-auth :endpoint="backend['endpoint']"/></b-card-text></b-tab>
              <b-tab title="JWT Token"><b-card-text><jwt-token /></b-card-text></b-tab>
            </b-tabs>
          </b-card>
        </b-col>
      </b-row>
    </div>
  </div>
</template>

<script>

import BasicAuth from '@/components/login/BasicAuth.vue'
import JwtToken from '@/components/login/JwtToken.vue'
import { API } from '@/api'

export default {
  name: 'Login',
  components: {
    BasicAuth,
    JwtToken
  },
  mounted () {
    localStorage.setItem('refreshed', false)
    this.list_backends()
  },
  data () {
    return {
      auth_backends: [],
      default_backend: '',
      tabIndex: 0,
    }
  },
  methods: {
    list_backends() {
      console.log("Listing auth backends")
      API
        .get(`/login`)
        .then(response => {
          console.log(response)
          if (response !== undefined) {
            this.auth_backends = response.data.data.backends
            this.default_backend = response.data.data.default
            var index = this.auth_backends.findIndex(b => b['endpoint'] == this.default_backend) || 0
            this.auth_backends.splice(0, 0, this.auth_backends.splice(index, 1)[0]);
            this.$nextTick(() => {
              this.tabIndex = 0
            })
          }
        })
        .catch(error => console.log(error))
    },
  },
}
</script>
