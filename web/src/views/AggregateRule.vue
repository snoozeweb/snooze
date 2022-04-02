<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="aggregaterule"
      edit_mode
      delete_mode
      add_mode
    >
      <template #details_side="row">
        <AuditLogs collection="aggregaterule" :object="row.item" />
      </template>
    </List>
  </div>
</template>

<script>
import AuditLogs from '@/components/AuditLogs.vue'
import List from '@/components/List.vue'

export default {
  components: {
    AuditLogs,
    List,
  },
}
</script>
