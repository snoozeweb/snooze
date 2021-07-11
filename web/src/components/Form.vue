<template>
  <div>
    <div v-for="(v, k) in metadata" :key="k">
      <CDropdownDivider v-if="k.includes('sweb_separator')"/>
      <h4 v-else-if="k.includes('sweb_title')"><CDropdownDivider/>{{v}}<CDropdownDivider/></h4>
      <Base v-else :metadata="v" v-model="value[k]" :data="value" class="pb-2" :ref="'form.' + k"/>
    </div>
  {{ value }}
  </div>
</template>

<script>

import Base from './form/Base.vue'

export default {
  components: { Base },
  props: ['value', 'metadata'],
}
</script>
