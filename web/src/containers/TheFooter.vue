<template>
  <CFooter :fixed="false">
    <div>
      <a href="https://github.com/rudexi/snooze" target="_blank">Snooze</a>
      <span class="ml-1">&copy; {{new Date().getFullYear()}} Snooze Team.</span>
    </div>
    <div class="mfs-auto">
      <span class="mr-1" target="_blank">Powered by</span>
      <a href="https://github.com/rudexi/snooze-web">Snooze-web</a>
    </div>
  </CFooter>
</template>

<script>
export default {
  name: 'TheFooter'
}
</script>
