<template>
  <div>
    <div v-for="(v, k) in metadata" :key="'f.' + k">
      <CDropdownDivider v-if="k.includes('sweb_separator')"/>
      <h4 v-else-if="k.includes('sweb_title')"><CDropdownDivider/>{{v}}<CDropdownDivider/></h4>
      <Base v-else :metadata="v" v-model="modelValue[k]" :data="modelValue" class="pb-2" :ref="'form.' + k"/>
    </div>
    <div v-if="footer_metadata" class="bg-light border pt-2">
      <div v-for="(v, k) in footer_metadata" :key="'p.' + k" class="px-2 pb-2">
        <CDropdownDivider v-if="k.includes('sweb_separator')"/>
        <h4 v-else-if="k.includes('sweb_title')"><CDropdownDivider/>{{v}}<CDropdownDivider/></h4>
        <FormFooter v-else :metadata="v" :data="modelValue" :ref="'footer.' + k"/>
      </div>
    </div>
  </div>
</template>

<script>

import Base from './form/Base.vue'
import FormFooter from './footer/FormFooter.vue'

export default {
  components: { Base, FormFooter },
  props: ['modelValue', 'metadata', 'footer_metadata'],
}
</script>
