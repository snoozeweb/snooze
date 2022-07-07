import { defineComponent, h, onMounted, ref, resolveComponent } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { safe_jwt_decode } from '@/utils/api'
import SScrollbar from '@/components/SScrollbar.vue'

import {
  CBadge,
  CSidebarNav,
  CNavItem,
  CNavGroup,
  CNavTitle,
} from '@coreui/vue'
import nav from '@/_nav.js'

const normalizePath = (path) =>
  decodeURI(path)
    .replace(/#.*$/, '')
    .replace(/(index)?\.(html)$/, '')

const isActiveLink = (route, link) => {
  if (link === undefined) {
    return false
  }

  if (route.hash === link) {
    return true
  }

  const currentPath = normalizePath(route.path)
  const targetPath = normalizePath(link)

  return currentPath === targetPath
}

const isActiveItem = (route, item) => {
  if (isActiveLink(route, item.to)) {
    return true
  }

  if (item.items) {
    return item.items.some((child) => isActiveItem(route, child))
  }

  return false
}

const nav_filter = (nav_el) => {
  var token = localStorage.getItem('snooze-token')
  var permissions = []
  if (token) {
    var decoded_token = safe_jwt_decode(token)
    if (decoded_token) {
      permissions = decoded_token.permissions
    } else {
      return
    }
  }
  if (permissions) {
    var nav_items = []
    nav_el.forEach(function(item) {
      if (item.permissions) {
        if (permissions.filter(cap => cap == 'rw_all' || cap == 'ro_all' || item.permissions.includes(cap)).length > 0) {
          nav_items.push(item)
        }
      } else {
        nav_items.push(item)
      }
    })
    var nav_children = []
    nav_items.forEach(function (item, i) {
      if (item.component != 'CNavTitle' || nav_items[i+1].component != 'CNavTitle') {
        nav_children.push(item)
      }
    })
    return nav_children
  } else {
    return []
  }
}

const AppSidebarNav = defineComponent({
  name: 'AppSidebarNav',
  components: {
    CNavItem,
    CNavGroup,
    CNavTitle,
    SScrollbar,
  },
  setup() {
    const route = useRoute()
    const firstRender = ref(true)

    onMounted(() => {
      firstRender.value = false
    })

    const renderItem = (item) => {
      if (item.items) {
        return h(
          CNavGroup,
          {
            ...(firstRender.value && {
              visible: item.items.some((child) => isActiveItem(route, child)),
            }),
          },
          {
            togglerContent: () => [
              h('i', {
                class: 'nav-icon ' + item.fontIcon,
              }),
              item.name,
            ],
            default: () => item.items.map((child) => renderItem(child)),
          },
        )
      }

      return item.to
        ? h(
            RouterLink,
            {
              to: item.to,
              custom: true,
            },
            {
              default: (props) =>
                h(
                  resolveComponent(item.component),
                  {
                    active: props.isActive,
                    href: props.href,
                  },
                  {
                    default: () => [
                      item.fontIcon &&
                        h('i', {
                          class: 'nav-icon ' + item.fontIcon,
                        }),
                      item.name,
                      item.badge &&
                        h(
                          CBadge,
                          {
                            class: 'ms-auto',
                            color: item.badge.color,
                          },
                          {
                            default: () => item.badge.text,
                          },
                        ),
                    ],
                  },
                ),
            },
          )
        : h(
            resolveComponent(item.component),
            {},
            {
              default: () => item.name,
            },
          )
    }

    return () =>
      h(
        CSidebarNav,
        {},
        {
          default: () =>
            h(
              SScrollbar,
              {},
              {
                default: () => nav_filter(nav).map((item) => renderItem(item)),
              }
            ),
        },
      )
  },
})
export { AppSidebarNav }
