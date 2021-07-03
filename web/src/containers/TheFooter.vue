<template>
  <CFooter :fixed="false">
    <div>
      <a href="https://snoozeweb.io" target="_blank">Snooze Web</a>
      <span class="ml-1">{{new Date()}}</span>
    </div>
    <div class="mfs-auto">
      <span class="mr-1" target="_blank">Contribute on</span>
      <a href="https://github.com/snooze">GitHub</a>
    </div>
  </CFooter>
</template>

<script>
export default {
  name: 'TheFooter'
}
</script>
