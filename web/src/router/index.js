import Vue from 'vue'
import Router from 'vue-router'

// Containers
const TheContainer = () => import('@/containers/TheContainer')
// Components
const Aggregate = () => import('@/views/Aggregate')
const Record = () => import('@/views/Record')
const Rule = () => import('@/views/Rule')
const AggregateRule = () => import('@/views/AggregateRule')
const Snooze = () => import('@/views/Snooze')
const Notification = () => import('@/views/Notification')
const Command = () => import('@/views/Command')
const User = () => import('@/views/User')
const Role = () => import('@/views/Role')
const Settings = () => import('@/views/Settings')
const Profile = () => import('@/views/Profile')
const Login = () => import('@/views/Login')

Vue.use(Router)

export default new Router({
  mode: 'hash', // https://router.vuejs.org/api/#mode
  linkActiveClass: 'active',
  scrollBehavior: () => ({ y: 0 }),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: Login
    },
    {
      path: '/',
      redirect: '/aggregate',
      name: 'Home',
      component: TheContainer,
      children: [
        {
          path: 'aggregate',
          name: 'Aggregates',
          component: Aggregate
        },
        {
          path: 'record',
          name: 'Records',
          component: Record
        },
        {
          path: 'rule',
          name: 'Rules',
          component: Rule
        },
        {
          path: 'aggregaterule',
          name: 'Aggregate Rules',
          component: AggregateRule
        },
        {
          path: 'snooze',
          meta: {label: 'Snooze'},
          component: Snooze,
        },
        {
          path: 'snooze/*',
          meta: {label: 'Snooze'},
          component: Snooze,
        },
        {
          path: '/notification',
          name: 'Notifications',
          component: Notification,
        },
        {
          path: '/command',
          name: 'Commands',
          component: Command,
        },
        {
          path: '/user',
          name: 'Users',
          component: User,
        },
        {
          path: '/role',
          name: 'Roles',
          component: Role,
        },
        {
          path: '/settings',
          name: 'Settings',
          component: Settings,
        },
        {
          path: '/profile',
          name: 'Profile',
          component: Profile,
        },
      ]
    },
  ]
})
