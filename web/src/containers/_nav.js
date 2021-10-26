export default [
  {
    _name: 'CSidebarNav',
    _children: [
      {
        _name: 'CSidebarNavItem',
        name: 'Alerts',
        to: '/record',
        fontIcon: 'la la-file-text',
	permissions: ['ro_record', 'rw_record'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Dashboard',
        to: '/dashboard',
        fontIcon: 'la la-tachometer-alt',
	permissions: ['ro_stats', 'rw_stats'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Status',
        to: '/status',
        fontIcon: 'la la-signal',
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Core']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Rules',
        to: '/rule',
        fontIcon: 'la la-balance-scale',
	permissions: ['ro_rule', 'rw_rule'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Aggregate Rules',
        to: '/aggregaterule',
        fontIcon: 'la la-suitcase',
	permissions: ['ro_aggregaterule', 'rw_aggregaterule'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Snooze',
        to: '/snooze',
        fontIcon: 'la la-bell-slash',
	permissions: ['ro_snooze', 'rw_snooze'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Notifications',
        to: '/notification',
        fontIcon: 'la la-bullhorn',
	permissions: ['ro_notification', 'rw_notification'],
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Utils']
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Actions',
        to: '/action',
        fontIcon: 'la la-code',
	permissions: ['ro_action', 'rw_action'],
      },
      {
        _name: "CSidebarNavItem",
        name: "Widgets",
        to: '/widget',
        fontIcon: 'la la-plug',
        permissions: ['ro_widget', 'rw_widget']
      },
      {
        _name: 'CSidebarNavTitle',
        _children: ['Customize'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Users',
        to: '/user',
        fontIcon: 'la la-users',
	permissions: ['ro_user', 'rw_user'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Roles',
        to: '/role',
        fontIcon: 'la la-user-friends',
	permissions: ['ro_role', 'rw_role'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Environments',
        to: '/environment',
        fontIcon: 'la la-layer-group',
	permissions: ['ro_environment', 'rw_environment'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Settings',
        to: '/settings',
        fontIcon: 'la la-cog',
	permissions: ['ro_settings', 'rw_settings'],
      },
      {
        _name: 'CSidebarNavItem',
        name: 'Profile',
        to: '/profile',
        fontIcon: 'la la-sliders',
      },
    ]
  }
]
