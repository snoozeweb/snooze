<template>
  <div v-if="show_login" class="app bg-light min-vh-100 d-flex flex-row align-items-center">
    <CContainer>
      <CRow class="justify-content-center px-5 pb-5">
        <img src="img/logo.png" style="max-width: 50%; height: auto;">
      </CRow>
      <CRow class="justify-content-center">
        <CCol md="8">
          <CCard>
            <CCardHeader class="card-header-border">
              <CNav variant="pills" role="tablist" card v-model="tabIndex">
                <CNavItem v-for="(backend, i) in this.auth_backends" :key="backend['endpoint']">
                  <CNavLink href="javascript:void(0);" :active="tabIndex == i" @click="() => {tabIndex = i}">{{ backend['name'] }}</CNavLink>
                </CNavItem>
                <CNavItem title="JWT Token">
                  <CNavLink href="javascript:void(0);" :active="tabIndex == this.auth_backends.length" @click="() => {tabIndex = this.auth_backends.length}">JWT Token</CNavLink>
                </CNavItem>
              </CNav>
            </CCardHeader>
            <CCardBody>
              <CTabContent>
                <CTabPane role="tabpanel" v-for="(backend, i) in this.auth_backends" :key="i" :visible="tabIndex == i">
                  <CCardText><basic-auth :endpoint="backend['endpoint']"/></CCardText>
                </CTabPane>
                <CTabPane role="tabpanel" :visible="tabIndex == this.auth_backends.length">
                  <CCardText><jwt-token /></CCardText>
                </CTabPane>
              </CTabContent>
            </CCardBody>
          </CCard>
        </CCol>
      </CRow>
    </CContainer>
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
      show_login: false,
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
            if (response.data.token !== undefined) {
              localStorage.setItem('snooze-token', response.data.token)
              if (this.$route.query.return_to) {
                this.$router.push(decodeURIComponent(this.$route.query.return_to))
              } else {
                this.$router.push('/record')
              }
            } else {
              this.auth_backends = response.data.data.backends
              this.show_login = true
            }
          }
        })
        .catch(error => console.log(error))
    },
  },
}
</script>
