<template>
  <DraggableTable
    endpoint_prop="rule"
  >
    <template #details_side="row">
      <AuditLogs collection="rule" :object="row.item" />
    </template>
  </DraggableTable>
</template>

<script>

import DraggableTable from '@/components/DraggableTable.vue'
import AuditLogs from '@/components/AuditLogs.vue'

export default {
  components: {
    DraggableTable,
    AuditLogs,
  },
  data () {
    return {
    }
  },
  mounted () {
  },
  methods: {
  },
}
</script>
