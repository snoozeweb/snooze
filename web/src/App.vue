<template>
  <router-view></router-view>
</template>

<script>
export default {
  name: 'App'
}
</script>

<style lang="scss">
  /* Import Font Awesome Icons Set */
  @import '~line-awesome/dist/line-awesome/css/line-awesome.min.css';
  /* Import Flag Icons Set */
  @import '~flag-icon-css/css/flag-icon.min.css';
  /* Import Bootstrap Vue Styles */
  @import '~bootstrap-vue/dist/bootstrap-vue.css';
  /* Import Vue Swatches Styles */
  @import '~vue-swatches/dist/vue-swatches.css';
  // Import Main styles for this application
  @import 'assets/scss/style';
</style>
