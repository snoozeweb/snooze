<template>
  <CDropdown
    inNav
    class="c-header-nav-items"
    placement="bottom-end"
    add-menu-classes="pt-0"
  >
    <template #toggler>
      <CHeaderNavLink>
        <div class="c-avatar">
          <span class="la-stack la-3x">
            <i class="las la-circle la-stack-1x"></i>
            <i class="las la-user-circle la-stack-1x"></i>
          </span>
        </div>
      </CHeaderNavLink>
    </template>
    <CDropdownHeader tag="div" class="text-center" color="light">
      <strong>Account</strong>
    </CDropdownHeader>
    <div class="pl-2 pr-2 pt-2">
      <span v-if="display_name" class="text-nowrap large">Hi <strong>{{ display_name }}</strong>!<br/>
        <CDropdownDivider/>
      </span>
      <span class="text-nowrap"><i class="la la-user-circle la-lg"/> {{ username }}<br/></span>
      <span v-if="email" class="text-nowrap font-italic"><i class="la la-at la-lg"/> {{ email }}</br></span>
      <CDropdownDivider/>
      <div class="align-middle">
      <span class="text-nowrap h6">Auth / Roles<br/></span>
      <CBadge :color="get_color(method)" class="mr-1">{{ method }}</CBadge>
      <CBadge v-for="field in roles" :key="field" :color="get_color(field)" class="mr-1">{{ field }}</CBadge>
      <span v-if="permissions.length > 0">
        <CDropdownDivider />
        <span class="text-nowrap h6">Permissions<br/></span>
        <CBadge v-for="field in permissions" :key="field" :color="get_color(field)" class="mr-1">{{ field }}</CBadge>
      </span>
      </div>
    </div>
    <CDropdownDivider/>
    <CDropdownItem @click="logout()">
      <i class="la la-sign-out-alt la-lg pr-2" /> Logout
    </CDropdownItem>
  </CDropdown>
</template>

<script>
import router from '@/router'
import { API } from '@/api'
import jwt_decode from "jwt-decode"
import { get_color } from '@/utils/colors'

export default {
  name: 'TheHeaderDropdownAccnt',
  data () {
    return {
      get_color: get_color,
      username: '',
      roles: [],
      permissions: [],
      method: '',
      display_name: '',
      email: '',
    }
  },
  methods: {
    get_data() {
      API
        .get('/profile_self/general')
        .then(response => {
          console.log(response)
          if (response.data) {
            this.display_name = response.data.data[0]['display_name'] || ''
            this.email = response.data.data[0]['email'] || ''
            localStorage.setItem('display_name', this.display_name)
            localStorage.setItem('email', this.email)
            localStorage.setItem('refreshed', true)
          }
        })
        .catch(error => console.log(error))
    },
    logout() {
      localStorage.setItem('snooze-token', '')
      router.push('/login')
    }
  },
  mounted() {
    var token = localStorage.getItem('snooze-token')
    if (token) {
      var decoded_token = jwt_decode(token)
      this.username = decoded_token.user.name
      this.method = decoded_token.user.method
      this.roles = decoded_token.user.roles
      this.permissions = decoded_token.user.permissions
      localStorage.setItem('name', this.username)
      localStorage.setItem('method', this.method)
      localStorage.setItem('roles', this.roles)
      localStorage.setItem('permissions', this.permissions)
      console.log(decoded_token)
    }
    if (localStorage.getItem('refreshed') != true) {
      this.get_data()
    } else {
      this.display_name = localStorage.getItem('display_name')
      this.email = localStorage.getItem('email')
    }
  }
}
</script>

<style scoped>
  .c-icon {
    margin-right: 0.3rem;
  }
</style>
