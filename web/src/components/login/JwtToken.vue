<template>
  <b-form>
    <p class="text-muted">Please enter your JWT token</p>
    <b-input-group class="mb-4">
      <b-input-group-prepend><b-input-group-text><i class="la la-lock la-lg"></i></b-input-group-text></b-input-group-prepend>
      <b-form-input v-model="token" type="password" class="login" placeholder="JWT Token" />
    </b-input-group>
    <b-row>
      <b-col cols="6">
        <b-button v-on:click="login" variant="primary" class="px-4">Login</b-button>
      </b-col>
    </b-row>
  </b-form>
</template>
<script>
import router from '@/router'
//import jwt_decode from "jwt-decode"

export default {
  name: 'JwtToken',
  data () {
    return {token: ""}
  },
  methods: {
    login: function () {
      console.log("Got authentication token")
      localStorage.setItem('snooze-token', this.token)
      //localStorage.setItem('username', jwt_decode(this.token)['user'])
      router.push('/aggregate')
    }
  },
}
</script>
