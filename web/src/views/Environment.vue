<template>
  <DraggableTable
    endpoint_prop="environment"
    max_level=1
  >
    <template #details_side="row">
      <AuditLogs collection="environment" :object="row.item" />
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
