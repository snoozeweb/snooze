<template>
  <router-view></router-view>
  <CToaster placement="top-end">
    <CToast v-for="(toast, index) in toasts" autohide :color="toast.variant" class="text-white">
      <CToastHeader closeButton>
        <span class="me-auto fw-bold text-white">{{toast.title}}</span>
      </CToastHeader>
      <CToastBody class="toast-body-extra">
        {{ toast.text }}
      </CToastBody>
    </CToast>
  </CToaster>
  <CAlert
    :visible="alert_timeout != null"
    dismissible
    fade
    class="position-fixed fixed-top m-0 rounded-0 text-center fade show"
    style="z-index: 2000;"
    color="success"
    ref="topalert"
    @close="() => {alert_timeout = null}"
  >
    Updated
  </CAlert>
</template>

<script>
export default {
  name: 'App',
  data () {
    return {
      toasts: [],
      alert_timeout: null,
    }
  },
  methods: {
    // Alert the user of a problem
    text_alert (text, variant = null, title = null) {
      if (title == null) {
        switch (variant) {
          case 'success':
            title = 'Success!'
            break
          case 'warning':
            title = 'Warning!'
            break
          case 'danger':
            title = 'Error!'
            break
          default:
            title = ''
        }
      }
      this.toasts.push({
        title: title,
        text: text,
        variant: variant,
      })
    },
    show_alert () {
      if (this.alert_timeout) {
        clearTimeout(this.alert_timeout)
        this.alert_timeout = null
      }
      this.alert_timeout = setTimeout(() => this.alert_timeout = null, 1000)
    },
  },
}
</script>

<style lang="scss">
  // Import Main styles for this application
  @import 'assets/scss/style';
  /* Import Font Awesome Icons Set */
  @import '~line-awesome/dist/line-awesome/css/line-awesome.min.css';
  // V-contextmenu css
  @import '~v-contextmenu/dist/themes/default.css';
</style>
